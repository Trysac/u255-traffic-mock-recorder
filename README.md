# traffic-mock-recorder

Record outbound HTTP traffic, redact credentials, generalize path IDs, and serve the result as a mock `http.Handler`.

`traffic-mock-recorder` is a Go library for integration testing. It observes real HTTP exchanges through a custom `http.RoundTripper`, stores them in a bounded ring buffer, redacts values of configured query parameters, drops named response headers, synthesizes path templates like `/users/{int}` or `/orders/{uuid}`, and provides an `http.Handler` that replays the latest recorded response per route. Request headers are never stored. Standard library only.

## Installation

```bash
go get github.com/Trysac/u255-traffic-mock-recorder
```

## Quick start

```go
package main
import ("net/http"; trafficmockrecorder "github.com/Trysac/u255-traffic-mock-recorder")
func main() {
    rec := trafficmockrecorder.NewRecorder(trafficmockrecorder.Options{})
    c := &http.Client{Transport: rec.RoundTripper(nil)}
    resp, _ := c.Get("https://api.example.com/users/7?token=secret")
    resp.Body.Close()
    _ = trafficmockrecorder.NewMockHandler(trafficmockrecorder.Synthesize(rec.Exchanges()))
}
```

## Public API

- `type Options struct { MaxExchanges int; MaxBodyBytes int; RedactQuery []string }`. `MaxExchanges` zero means 1000. `MaxBodyBytes` zero means 1_048_576. `RedactQuery` zero means `token`, `access_token`, `api_key`, `apikey`, `key`, `secret`, `password`, `signature`; names are compared case-insensitively after unescaping.
- `NewRecorder(opts Options) *Recorder`. `RoundTripper(next http.RoundTripper) http.RoundTripper`: nil `next` means `http.DefaultTransport`; a transport error from `next` is returned unchanged and nothing is recorded. Request and response bodies are read up to `MaxBodyBytes+1` and handed back through an `io.MultiReader` of the buffered prefix and the rest, so callers and servers receive identical bytes; a body over the cap is not stored and sets `Truncated`.
- `Exchanges() []Exchange`: a copy, oldest first. `Exchange` fields are `Seq`, `Method`, `Path`, `Query`, `RequestBody`, `Status`, `ResponseHeader`, `ResponseBody`, `Truncated`; JSON tags are `seq`, `method`, `path`, `query`, `request_body,omitempty`, `status`, `response_header`, `response_body`, `truncated`.
- `Dropped() int`: number of oldest exchanges discarded by the ring.
- `Synthesize(exchanges []Exchange) []Stub`: skips `Truncated`; groups by method and template; the exchange with the highest `Seq` supplies status, content type, and body; `Hits` counts the group; stubs are sorted by method then template. A path segment made only of ASCII digits becomes `{int}`; a canonical UUID (8-4-4-4-12 hex, any case) becomes `{uuid}`; other segments stay literal.
- `NewMockHandler(stubs []Stub) http.Handler`: ignores the query string; a stub matches when every segment matches; the stub with more literal segments wins, ties go to slice order. No match returns 501 with `Content-Type: application/json` and body `{"error":"no stub","method":"<METHOD>","path":"<PATH>"}`.
- `Stub` fields are `Method`, `Template`, `Status`, `ContentType`, `Body`, `Hits` with snake_case JSON tags.
- Query redaction splits the raw query on `&`; a pair whose unescaped name is in `RedactQuery` becomes `name=REDACTED`; pairs without `=` are kept. Request headers are never stored; `Set-Cookie`, `WWW-Authenticate`, and `Proxy-Authenticate` are dropped from stored response headers.

## Features

- **Recording RoundTripper**: wraps any `http.RoundTripper` (defaults to `http.DefaultTransport`), captures request/response metadata and bodies, with configurable limits.
- **Query redaction**: redacts values of specified query parameters (default list), preserving order and handling missing `=`.
- **Header filtering**: never stores request headers; drops `Set-Cookie`, `WWW-Authenticate`, and `Proxy-Authenticate` from stored response headers.
- **Ring buffer**: bounded by `MaxExchanges`; oldest exchanges are dropped and counted.
- **Path templating**: digit-only segments become `{int}`, canonical UUIDs become `{uuid}`.
- **Mock handler**: matches templates against request paths (ignoring query), prefers more literal segments, returns 501 JSON when no stub matches.
- **Thread-safe**: the recorder is mutex-protected and safe for concurrent use.

## Why not exact-match cassettes

Exact record-and-replay cassettes break when an ID in the path changes. This library generalizes digit-only and UUID path segments into templates, so a stub for `/users/{int}` matches `/users/7` and `/users/12`, and the mock is an ordinary `http.Handler` for `httptest`. Unlike a webhook relay, it never forwards traffic; it only observes and replays recorded responses.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Security](docs/SECURITY.md)
- [Testing](docs/TESTING.md)
- [Project](docs/PROJECT.md)
- [Stack](docs/STACK.md)

## License

See [LICENSE](LICENSE).
