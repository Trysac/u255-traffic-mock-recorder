# Testing

Run the full test suite:

```bash
go test ./...
```

Including race detector:

```bash
go test -race ./...
```

The tests are Go `_test.go` files in the package. They cover:
- RoundTripper recording through `rec.RoundTripper(nil)`: method, path, redacted query, status, and response body byte-identical to the caller.
- Query redaction for configured names, including case-insensitive matching, order preservation, and a pair without `=`.
- Header filtering: no request header is stored; `Set-Cookie` is removed from stored response headers; serialized `Exchanges()` does not contain a sent secret.
- Body caps: a body over `MaxBodyBytes` reaches the caller complete while the exchange has `Truncated` true and an empty `ResponseBody`; `Synthesize` skips truncated exchanges.
- Ring buffer overflow: with `MaxExchanges: 2`, three requests keep the second and third and report `Dropped() == 1`.
- Synthesis grouping and templating: literal, `{int}`, and `{uuid}` templates; latest highest `Seq` supplies status, content type, and body; `Hits` counts; sort by method then template.
- Mock handler matching: literal beats `{int}`, `{int}` and `{uuid}` matches, and a no-match request returns 501 JSON.
- Edge cases: root path `/`, 204 response with no body, POST body larger than the cap still reaching the server intact.
- Concurrency: `go test -race` passes with 50 concurrent requests recording 50 exchanges with distinct `Seq` values 1..50.
