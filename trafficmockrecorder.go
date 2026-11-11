package trafficmockrecorder

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Options configures a Recorder.
type Options struct {
	// MaxExchanges is the maximum number of exchanges to retain.
	// When 0 or negative, the default of 1000 is used.
	MaxExchanges int

	// MaxBodyBytes is the maximum number of bytes of request/response bodies to store.
	// When 0 or negative, the default of 1,048,576 (1 MiB) is used.
	MaxBodyBytes int

	// RedactQuery lists query parameter names whose values should be redacted.
	// When nil, a default set is used: token, access_token, api_key, apikey, key,
	// secret, password, signature. Comparison is case-insensitive.
	RedactQuery []string
}

// Exchange is a single recorded HTTP interaction.
type Exchange struct {
	Seq            int         `json:"seq"`
	Method         string      `json:"method"`
	Path           string      `json:"path"`
	Query          string      `json:"query"`
	RequestBody    []byte      `json:"request_body,omitempty"`
	Status         int         `json:"status"`
	ResponseHeader http.Header `json:"response_header"`
	ResponseBody   []byte      `json:"response_body"`
	Truncated      bool        `json:"truncated"`
}

// Stub is a synthesized mock endpoint from recorded exchanges.
type Stub struct {
	Method      string `json:"method"`
	Template    string `json:"template"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
	Hits        int    `json:"hits"`
}

// Recorder captures HTTP exchanges made through a RoundTripper.
type Recorder struct {
	mu      sync.Mutex
	opts    Options
	redact  map[string]struct{}
	buffer  []Exchange
	start   int
	count   int
	dropped int
	nextSeq int
}

// NewRecorder creates a Recorder with the given options.
func NewRecorder(opts Options) *Recorder {
	if opts.MaxExchanges <= 0 {
		opts.MaxExchanges = 1000
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 1_048_576
	}
	if opts.RedactQuery == nil {
		opts.RedactQuery = []string{"token", "access_token", "api_key", "apikey", "key", "secret", "password", "signature"}
	}
	redact := make(map[string]struct{}, len(opts.RedactQuery))
	for _, name := range opts.RedactQuery {
		redact[strings.ToLower(name)] = struct{}{}
	}
	return &Recorder{
		opts:    opts,
		redact:  redact,
		buffer:  make([]Exchange, opts.MaxExchanges),
		nextSeq: 1,
	}
}

// RoundTripper returns an http.RoundTripper that records exchanges and
// forwards requests to next, or to http.DefaultTransport if next is nil.
func (r *Recorder) RoundTripper(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return r.roundTrip(req, next)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (r *Recorder) roundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	clone := req.Clone(req.Context())
	var reqPrefix []byte
	var reqTruncated bool
	if clone.Body != nil {
		var err error
		reqPrefix, reqTruncated, err = readBodyPrefix(clone.Body, r.opts.MaxBodyBytes)
		if err != nil {
			return nil, err
		}
		clone.Body = &readCloser{
			Reader: io.MultiReader(bytes.NewReader(reqPrefix), clone.Body),
			closer: clone.Body,
		}
	}

	method := req.Method
	path := req.URL.Path
	redactedQuery := r.redactQuery(req.URL.RawQuery)

	resp, err := next.RoundTrip(clone)
	if err != nil {
		return nil, err
	}

	var respPrefix []byte
	var respTruncated bool
	if resp.Body != nil {
		respPrefix, respTruncated, err = readBodyPrefix(resp.Body, r.opts.MaxBodyBytes)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body = &readCloser{
			Reader: io.MultiReader(bytes.NewReader(respPrefix), resp.Body),
			closer: resp.Body,
		}
	}

	respHeader := resp.Header.Clone()
	dropCredentialHeaders(respHeader)

	ex := Exchange{
		Method:         method,
		Path:           path,
		Query:          redactedQuery,
		Status:         resp.StatusCode,
		ResponseHeader: respHeader,
	}
	if !reqTruncated {
		ex.RequestBody = reqPrefix
	} else {
		ex.Truncated = true
	}
	if !respTruncated {
		ex.ResponseBody = respPrefix
	} else {
		ex.Truncated = true
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	ex.Seq = r.nextSeq
	r.nextSeq++
	if r.count < r.opts.MaxExchanges {
		idx := (r.start + r.count) % r.opts.MaxExchanges
		r.buffer[idx] = ex
		r.count++
	} else {
		r.buffer[r.start] = ex
		r.start = (r.start + 1) % r.opts.MaxExchanges
		r.dropped++
	}

	return resp, nil
}

// dropCredentialHeaders removes credential-bearing response headers from h,
// matching header names case-insensitively so non-canonical keys are removed too.
func dropCredentialHeaders(h http.Header) {
	for key := range h {
		if strings.EqualFold(key, "Set-Cookie") ||
			strings.EqualFold(key, "WWW-Authenticate") ||
			strings.EqualFold(key, "Proxy-Authenticate") {
			delete(h, key)
		}
	}
}

// readBodyPrefix reads up to max+1 bytes from r, returning the bytes actually
// read and whether the input held more than max bytes. When the body is over
// the cap the returned prefix is still handed back to the caller through an
// io.MultiReader, it is just not stored in the Exchange.
func readBodyPrefix(r io.Reader, max int) ([]byte, bool, error) {
	limited := io.LimitReader(r, int64(max+1))
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	return buf, len(buf) > max, nil
}

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (rc *readCloser) Close() error {
	return rc.closer.Close()
}

// redactQuery applies redaction to the raw query string, preserving order.
func (r *Recorder) redactQuery(raw string) string {
	if raw == "" || len(r.redact) == 0 {
		return raw
	}
	pairs := strings.Split(raw, "&")
	for i, pair := range pairs {
		if pair == "" {
			continue
		}
		eq := strings.Index(pair, "=")
		if eq == -1 {
			continue
		}
		name := pair[:eq]
		unescaped, err := url.QueryUnescape(name)
		if err != nil {
			continue
		}
		if _, ok := r.redact[strings.ToLower(unescaped)]; ok {
			pairs[i] = name + "=REDACTED"
		}
	}
	return strings.Join(pairs, "&")
}

// Exchanges returns a copy of all recorded exchanges in chronological order (oldest first).
func (r *Recorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Exchange, r.count)
	for i := 0; i < r.count; i++ {
		idx := (r.start + i) % r.opts.MaxExchanges
		out[i] = r.buffer[idx]
	}
	return out
}

// Dropped returns the number of exchanges dropped due to MaxExchanges overflow.
func (r *Recorder) Dropped() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Synthesize groups recorded exchanges into stubs, generalizing path segments.
// Exchanges with Truncated=true are skipped.
// The stub for each group uses the Status, ContentType and Body from the exchange
// with the highest Seq (latest). Hits is the number of exchanges in the group.
// Stubs are sorted by Method, then Template.
func Synthesize(exchanges []Exchange) []Stub {
	type groupInfo struct {
		exchange Exchange
		hits     int
	}
	groups := make(map[string]*groupInfo)
	order := make([]string, 0)
	for _, ex := range exchanges {
		if ex.Truncated {
			continue
		}
		tmpl := templateForPath(ex.Path)
		key := ex.Method + "\x00" + tmpl
		if _, ok := groups[key]; !ok {
			groups[key] = &groupInfo{exchange: ex, hits: 1}
			order = append(order, key)
		} else {
			groups[key].hits++
			if ex.Seq > groups[key].exchange.Seq {
				groups[key].exchange = ex
			}
		}
	}
	stubs := make([]Stub, 0, len(order))
	for _, key := range order {
		g := groups[key]
		ex := g.exchange
		stub := Stub{
			Method:      ex.Method,
			Template:    templateForPath(ex.Path),
			Status:      ex.Status,
			ContentType: ex.ResponseHeader.Get("Content-Type"),
			Body:        ex.ResponseBody,
			Hits:        g.hits,
		}
		stubs = append(stubs, stub)
	}
	sort.Slice(stubs, func(i, j int) bool {
		if stubs[i].Method != stubs[j].Method {
			return stubs[i].Method < stubs[j].Method
		}
		return stubs[i].Template < stubs[j].Template
	})
	return stubs
}

// NewMockHandler returns an http.Handler that serves matched stubs.
// A stub only matches requests with the same method. Matching ignores the query
// string; a stub matches when every path segment matches, with "{int}" matching
// all-digit segments and "{uuid}" matching canonical UUIDs. Among multiple
// matches, the stub with more literal segments wins; ties go to slice order. If
// no stub matches, it responds 501 with JSON.
func NewMockHandler(stubs []Stub) http.Handler {
	myStubs := make([]Stub, len(stubs))
	copy(myStubs, stubs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathSegs := splitPath(r.URL.Path)
		var bestIdx = -1
		var bestLiteral = -1
		for i, stub := range myStubs {
			if stub.Method != r.Method {
				continue
			}
			tmplSegs := splitPath(stub.Template)
			if len(pathSegs) != len(tmplSegs) {
				continue
			}
			literalCount := 0
			match := true
			for j, tmplSeg := range tmplSegs {
				seg := pathSegs[j]
				if tmplSeg == "{int}" {
					if !isInt(seg) {
						match = false
						break
					}
				} else if tmplSeg == "{uuid}" {
					if !isUUID(seg) {
						match = false
						break
					}
				} else {
					if seg != tmplSeg {
						match = false
						break
					}
					literalCount++
				}
			}
			if match {
				if literalCount > bestLiteral {
					bestLiteral = literalCount
					bestIdx = i
				}
			}
		}
		if bestIdx == -1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			payload, err := json.Marshal(struct {
				Error  string `json:"error"`
				Method string `json:"method"`
				Path   string `json:"path"`
			}{Error: "no stub", Method: r.Method, Path: r.URL.Path})
			if err != nil {
				return
			}
			w.Write(payload)
			return
		}
		stub := myStubs[bestIdx]
		if stub.ContentType != "" {
			w.Header().Set("Content-Type", stub.ContentType)
		}
		w.WriteHeader(stub.Status)
		if stub.Body != nil {
			w.Write(stub.Body)
		}
	})
}

// templateForPath converts a URL path into a template by replacing digit-only
// segments with "{int}" and canonical UUID segments with "{uuid}".
func templateForPath(path string) string {
	segs := splitPath(path)
	for i, seg := range segs {
		if isInt(seg) {
			segs[i] = "{int}"
		} else if isUUID(seg) {
			segs[i] = "{uuid}"
		}
	}
	if len(segs) == 0 {
		return "/"
	}
	return "/" + strings.Join(segs, "/")
}

// splitPath splits a path into segments, ignoring the leading slash.
// For path "/", it returns an empty slice.
func splitPath(path string) []string {
	if path == "/" {
		return []string{}
	}
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

// isInt reports whether s is non-empty and consists only of ASCII digits.
func isInt(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isUUID reports whether s is a canonical UUID (8-4-4-4-12 hex digits, any case).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !isHexByte(c) {
				return false
			}
		}
	}
	return true
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
