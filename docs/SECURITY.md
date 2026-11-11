# Security Considerations

## Threat model

The library is intended for test environments. It observes and records HTTP exchanges in memory and replays selected responses; it does not forward traffic.

### Defended

- Query redaction: values of query parameters whose unescaped names match `RedactQuery` are replaced with `REDACTED`. Names are compared case-insensitively. The default list is `token`, `access_token`, `api_key`, `apikey`, `key`, `secret`, `password`, `signature`. Raw query order is preserved; a pair without `=` is kept.
- Request headers are never stored.
- Response headers `Set-Cookie`, `WWW-Authenticate`, and `Proxy-Authenticate` are dropped before storage.
- Bodies are stored only up to `MaxBodyBytes` (default 1,048,576 bytes when zero). A body over the cap is not stored; the exchange has `Truncated` true and an empty `ResponseBody`. The caller and server still receive the complete body.
- History is bounded by `MaxExchanges` (default 1000 when zero); the oldest exchange is dropped and counted by `Dropped()`.

### Not defended

- Request and response bodies are stored verbatim up to the cap and are not scanned or redacted; they may contain credentials or personal data.
- Path segments and query parameters not named in `RedactQuery` are stored as-is.
- Response headers other than the three dropped names are stored.
- The library does not encrypt, persist, or control access to recorded exchanges; callers who serialize `Exchanges()` are responsible for the result.
- The library does not intercept HTTPS or act as a proxy, does not import HAR files, does not match on request bodies, does not simulate latency, and has no CLI.

## Recommendations

- Review recorded exchanges before using them outside test environments.
- Keep `MaxBodyBytes` and `MaxExchanges` as small as the test needs.
- Do not serialize or log exchanges without additional redaction.
