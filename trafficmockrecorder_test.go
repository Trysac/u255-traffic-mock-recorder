package trafficmockrecorder_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Trysac/u255-traffic-mock-recorder"
)

func TestRecorderRoundTripperBasic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("server got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/users/7" {
			t.Errorf("server got path %s, want /users/7", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":7}`))
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	resp, err := client.Get(server.URL + "/users/7?token=abc&page=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"id":7}` {
		t.Fatalf("body = %s, want %s", body, `{"id":7}`)
	}

	exchanges := rec.Exchanges()
	if len(exchanges) != 1 {
		t.Fatalf("len(Exchanges()) = %d, want 1", len(exchanges))
	}
	ex := exchanges[0]
	if ex.Method != "GET" {
		t.Errorf("Method = %s, want GET", ex.Method)
	}
	if ex.Path != "/users/7" {
		t.Errorf("Path = %s, want /users/7", ex.Path)
	}
	if ex.Query != "token=REDACTED&page=2" {
		t.Errorf("Query = %s, want token=REDACTED&page=2", ex.Query)
	}
	if ex.Status != 200 {
		t.Errorf("Status = %d, want 200", ex.Status)
	}
	if !bytes.Equal(ex.ResponseBody, []byte(`{"id":7}`)) {
		t.Errorf("ResponseBody = %s, want %s", ex.ResponseBody, `{"id":7}`)
	}
}

func TestRecorderRedactsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			t.Errorf("Authorization header not passed correctly")
		}
		w.Header().Set("Set-Cookie", "sid=s3cret")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	exchangesJSON, _ := json.Marshal(rec.Exchanges())
	if bytes.Contains(exchangesJSON, []byte("s3cret")) {
		t.Errorf("JSON contains secret: %s", exchangesJSON)
	}
	for _, ex := range rec.Exchanges() {
		if _, ok := ex.ResponseHeader["Set-Cookie"]; ok {
			t.Errorf("ResponseHeader contains Set-Cookie")
		}
	}
}

func TestRecorderDropsNonCanonicalCredentialHeaders(t *testing.T) {
	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"set-cookie":       []string{"sid=s3cret"},
				"www-authenticate": []string{"Basic realm=s3cret"},
			},
			Body:    io.NopCloser(strings.NewReader("ok")),
			Request: req,
		}, nil
	}))}
	resp, err := client.Get("http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	exchangesJSON, err := json.Marshal(rec.Exchanges())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(exchangesJSON, []byte("s3cret")) {
		t.Errorf("JSON contains secret: %s", exchangesJSON)
	}
	lower := strings.ToLower(string(exchangesJSON))
	if strings.Contains(lower, "set-cookie") || strings.Contains(lower, "www-authenticate") {
		t.Errorf("JSON contains credential header: %s", exchangesJSON)
	}
}

func TestSynthesizeUserExchanges(t *testing.T) {
	exchanges := []trafficmockrecorder.Exchange{
		{Seq: 1, Method: "GET", Path: "/users/7", Status: 200, ResponseHeader: http.Header{"Content-Type": []string{"application/json"}}, ResponseBody: []byte(`{"id":7}`)},
		{Seq: 2, Method: "GET", Path: "/users/12", Status: 200, ResponseHeader: http.Header{"Content-Type": []string{"application/json"}}, ResponseBody: []byte(`{"id":12}`)},
		{Seq: 3, Method: "GET", Path: "/users/me", Status: 200, ResponseHeader: http.Header{"Content-Type": []string{"application/json"}}, ResponseBody: []byte(`{"id":"me"}`)},
	}
	stubs := trafficmockrecorder.Synthesize(exchanges)
	if len(stubs) != 2 {
		t.Fatalf("len(stubs) = %d, want 2", len(stubs))
	}
	if stubs[0].Method != "GET" || stubs[0].Template != "/users/me" {
		t.Errorf("first stub = %+v, want GET /users/me", stubs[0])
	}
	if stubs[0].Hits != 1 {
		t.Errorf("first stub Hits = %d, want 1", stubs[0].Hits)
	}
	if stubs[1].Method != "GET" || stubs[1].Template != "/users/{int}" {
		t.Errorf("second stub = %+v, want GET /users/{int}", stubs[1])
	}
	if stubs[1].Hits != 2 {
		t.Errorf("second stub Hits = %d, want 2", stubs[1].Hits)
	}
	if !bytes.Equal(stubs[1].Body, []byte(`{"id":12}`)) {
		t.Errorf("stub body = %s, want %s", stubs[1].Body, `{"id":12}`)
	}
}

func TestTemplateUUID(t *testing.T) {
	exchanges := []trafficmockrecorder.Exchange{
		{Seq: 1, Method: "GET", Path: "/orders/3f2b1c9e-8d7a-4b6c-9e1f-2a3b4c5d6e7f", Status: 200, ResponseHeader: http.Header{}, ResponseBody: []byte(`ok`)},
	}
	stubs := trafficmockrecorder.Synthesize(exchanges)
	if len(stubs) != 1 {
		t.Fatalf("len(stubs) = %d, want 1", len(stubs))
	}
	if stubs[0].Template != "/orders/{uuid}" {
		t.Errorf("Template = %s, want /orders/{uuid}", stubs[0].Template)
	}
}

func TestNewMockHandler(t *testing.T) {
	stubs := []trafficmockrecorder.Stub{
		{Method: "GET", Template: "/users/{int}", Status: 200, ContentType: "application/json", Body: []byte(`{"stub":"int"}`)},
		{Method: "GET", Template: "/users/me", Status: 200, ContentType: "application/json", Body: []byte(`{"stub":"me"}`)},
	}
	handler := trafficmockrecorder.NewMockHandler(stubs)

	// Test matching int
	req := httptest.NewRequest(http.MethodGet, "/users/99", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("int match status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("int match content-type = %s, want application/json", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != `{"stub":"int"}` {
		t.Errorf("int match body = %s, want %s", rec.Body.String(), `{"stub":"int"}`)
	}

	// Test matching literal
	req = httptest.NewRequest(http.MethodGet, "/users/me", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("literal match status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != `{"stub":"me"}` {
		t.Errorf("literal match body = %s, want %s", rec.Body.String(), `{"stub":"me"}`)
	}

	// Test no match
	req = httptest.NewRequest(http.MethodPost, "/users/99", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("no match status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("no match content-type = %s, want application/json", rec.Header().Get("Content-Type"))
	}
	expectedBody := `{"error":"no stub","method":"POST","path":"/users/99"}`
	if rec.Body.String() != expectedBody {
		t.Errorf("no match body = %s, want %s", rec.Body.String(), expectedBody)
	}
}

func TestNewMockHandlerNoStubBodyIsValidJSON(t *testing.T) {
	handler := trafficmockrecorder.NewMockHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/a%22b", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	var decoded struct {
		Error  string `json:"error"`
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal 501 body %q: %v", rec.Body.String(), err)
	}
	if decoded.Error != "no stub" || decoded.Method != "GET" || decoded.Path != `/a"b` {
		t.Errorf("decoded = %+v, want error=no stub method=GET path=/a\"b", decoded)
	}
}

func TestMaxExchangesRing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{MaxExchanges: 2})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	for i := 0; i < 3; i++ {
		resp, err := client.Get(server.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	exchanges := rec.Exchanges()
	if len(exchanges) != 2 {
		t.Fatalf("len(Exchanges()) = %d, want 2", len(exchanges))
	}
	if rec.Dropped() != 1 {
		t.Fatalf("Dropped() = %d, want 1", rec.Dropped())
	}
	// The first exchange should have Seq=2 (since Seq starts at 1 and we did 3, first dropped)
	if exchanges[0].Seq != 2 {
		t.Errorf("first exchange Seq = %d, want 2", exchanges[0].Seq)
	}
	if exchanges[1].Seq != 3 {
		t.Errorf("second exchange Seq = %d, want 3", exchanges[1].Seq)
	}
}

func TestMaxBodyBytesTruncation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("1234567890")) // 10 bytes
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{MaxBodyBytes: 4})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "1234567890" {
		t.Fatalf("caller got %s, want full 10 bytes", body)
	}
	exchanges := rec.Exchanges()
	if len(exchanges) != 1 {
		t.Fatalf("len(Exchanges()) = %d, want 1", len(exchanges))
	}
	ex := exchanges[0]
	if !ex.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if len(ex.ResponseBody) != 0 {
		t.Errorf("ResponseBody length = %d, want 0", len(ex.ResponseBody))
	}
	stubs := trafficmockrecorder.Synthesize(exchanges)
	if len(stubs) != 0 {
		t.Errorf("Synthesize with truncated exchange returned %d stubs, want 0", len(stubs))
	}
}

func TestQueryRedactionEdgeCases(t *testing.T) {
	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}))}
	tests := []struct {
		raw  string
		want string
	}{
		{"token=abc&page=2", "token=REDACTED&page=2"},
		{"TOKEN=abc&page=2", "TOKEN=REDACTED&page=2"},
		{"api_key=secret&x=1", "api_key=REDACTED&x=1"},
		{"key=value", "key=REDACTED"},
		{"flag", "flag"}, // no '='
		{"", ""},
		{"a=1&b=2", "a=1&b=2"},
	}
	for _, tt := range tests {
		url := "http://example.com/path"
		if tt.raw != "" {
			url += "?" + tt.raw
		}
		before := len(rec.Exchanges())
		resp, err := client.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		resp.Body.Close()
		exchanges := rec.Exchanges()
		if len(exchanges) != before+1 {
			t.Fatalf("len(Exchanges()) = %d, want %d", len(exchanges), before+1)
		}
		if got := exchanges[before].Query; got != tt.want {
			t.Errorf("Query for %q = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestTransportError(t *testing.T) {
	errExpected := errors.New("transport failed")
	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errExpected
	})
	client := &http.Client{Transport: rec.RoundTripper(transport)}
	_, err := client.Get("http://example.com")
	if !errors.Is(err, errExpected) {
		t.Fatalf("error = %v, want %v", err, errExpected)
	}
	if len(rec.Exchanges()) != 0 {
		t.Errorf("exchanges recorded on error: %d", len(rec.Exchanges()))
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestConcurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(nil)}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL)
			if err != nil {
				t.Errorf("concurrent request error: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	exchanges := rec.Exchanges()
	if len(exchanges) != n {
		t.Fatalf("len(Exchanges()) = %d, want %d", len(exchanges), n)
	}
	seen := make(map[int]bool)
	for _, ex := range exchanges {
		if ex.Seq < 1 || ex.Seq > n {
			t.Errorf("Seq out of range: %d", ex.Seq)
		}
		if seen[ex.Seq] {
			t.Errorf("duplicate Seq: %d", ex.Seq)
		}
		seen[ex.Seq] = true
	}
	for i := 1; i <= n; i++ {
		if !seen[i] {
			t.Errorf("missing Seq: %d", i)
		}
	}
}

func TestRequestBodyLargeStillReachesServer(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{MaxBodyBytes: 4})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	largeBody := strings.Repeat("a", 100)
	resp, err := client.Post(server.URL, "text/plain", strings.NewReader(largeBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	actual := <-received
	if string(actual) != largeBody {
		t.Errorf("server got %d bytes, want %d", len(actual), len(largeBody))
	}
	exchanges := rec.Exchanges()
	if len(exchanges) != 1 {
		t.Fatalf("len(Exchanges()) = %d, want 1", len(exchanges))
	}
	ex := exchanges[0]
	if !ex.Truncated {
		t.Errorf("Truncated = false, want true (request body over cap)")
	}
	if len(ex.RequestBody) != 0 {
		t.Errorf("RequestBody length = %d, want 0", len(ex.RequestBody))
	}
}

func TestRootPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	exchanges := rec.Exchanges()
	if len(exchanges) != 1 {
		t.Fatalf("len(Exchanges()) = %d, want 1", len(exchanges))
	}
	if exchanges[0].Path != "/" {
		t.Errorf("Path = %s, want /", exchanges[0].Path)
	}
	stubs := trafficmockrecorder.Synthesize(exchanges)
	if len(stubs) != 1 {
		t.Fatalf("len(stubs) = %d, want 1", len(stubs))
	}
	if stubs[0].Template != "/" {
		t.Errorf("Template = %s, want /", stubs[0].Template)
	}
	handler := trafficmockrecorder.NewMockHandler(stubs)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("mock handler status for / = %d, want 200", rr.Code)
	}
}

func Test204NoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(nil)}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	exchanges := rec.Exchanges()
	if len(exchanges) != 1 {
		t.Fatalf("len(Exchanges()) = %d, want 1", len(exchanges))
	}
	ex := exchanges[0]
	if ex.Status != 204 {
		t.Errorf("Status = %d, want 204", ex.Status)
	}
	if len(ex.ResponseBody) != 0 {
		t.Errorf("ResponseBody length = %d, want 0", len(ex.ResponseBody))
	}
	if ex.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

func Example() {
	rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
	client := &http.Client{Transport: rec.RoundTripper(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"id":7}`)),
			Request:    req,
		}, nil
	}))}
	resp, err := client.Get("https://api.example.com/users/7?token=secret")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	exchanges := rec.Exchanges()
	fmt.Println(exchanges[0].Query)
	// Output: token=REDACTED
}
