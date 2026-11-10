# Architecture

The library has three independent parts: recording, synthesis, and serving.

1. **Recorder** — `NewRecorder(Options)` returns a mutex-protected `*Recorder`. `RoundTripper(next http.RoundTripper) http.RoundTripper` wraps `next`; a nil `next` uses `http.DefaultTransport`. For each exchange it reads request and response bodies up to `MaxBodyBytes+1` bytes and hands the same bytes back through an `io.MultiReader` of the buffered prefix and the remaining body, closing the original body. A body over the cap is not stored and sets `Truncated`. Request headers are never stored. Response headers `Set-Cookie`, `WWW-Authenticate`, and `Proxy-Authenticate` are dropped. Query redaction splits the raw query on `&`; a pair whose unescaped name is in `RedactQuery` becomes `name=REDACTED`; pairs without `=` are kept. `MaxExchanges` is a ring: the oldest exchange is dropped and counted by `Dropped()`. `Exchanges()` returns a copy oldest first. A transport error from `next` is returned unchanged and nothing is recorded.

2. **Synthesize** — a pure function `Synthesize([]Exchange) []Stub` that skips truncated exchanges and groups the rest by `(Method, Template)`. A path segment made only of ASCII digits becomes `{int}`; a canonical UUID (8-4-4-4-12 hex, any case) becomes `{uuid}`; other segments stay literal. In each group the exchange with the highest `Seq` supplies `Status`, `ContentType`, and `Body`; `Hits` counts the group. Stubs are sorted by method then template.

3. **NewMockHandler** — `NewMockHandler([]Stub) http.Handler` matches an incoming request path against stubs, ignoring the query string. A stub matches when every segment matches; `{int}` and `{uuid}` segments match their segment kinds. When several stubs match, the one with more literal segments wins; ties go to slice order. If none matches, the handler returns status 501 with `Content-Type: application/json` and a body `{"error":"no stub","method":"<METHOD>","path":"<PATH>"}`.

Design decisions:
- Recording, synthesis, and serving are independent; callers can use any combination.
- `MaxExchanges` and `MaxBodyBytes` bound memory; defaults are 1000 and 1,048,576 bytes when zero.
- No global state; options are per-recorder.
- Standard library only.
