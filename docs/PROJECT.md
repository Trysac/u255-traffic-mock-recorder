# Project

- Name: traffic-mock-recorder
- Module: `github.com/Trysac/u255-traffic-mock-recorder`
- Package: `trafficmockrecorder`
- Language: Go (>= 1.23)
- Dependencies: none (standard library only)
- License: [MIT](../LICENSE)

## Objective

Integration tests against third-party APIs either call the real service or use hand-written mocks that drift from real responses. Exact record-and-replay cassettes break when an ID in the path changes. This library records what the client actually exchanged, redacts credentials, generalizes IDs in paths into templates, and serves the latest observed response per route.

## Scope

In scope: a Go library with an `http.RoundTripper` recorder; query-parameter redaction; response-header filtering; bounded bodies and bounded exchange history; path-template synthesis for digit-only and canonical UUID segments; and an `http.Handler` that replays stubs for `httptest` and similar test servers.

## Out of scope

HTTPS interception proxies, HAR import, request-body matching, latency simulation, and a CLI.

## Factory run u255 (measured from API usage)

| Date (UTC) | Phase | Model | Batch | Input | Output | Cache write | Cache read | Cost (USD) |
|---|---|---|---|---|---|---|---|---|
| 2026-11-10 | generation | deepseek-v4-pro | yes | 1540 | 27333 | 0 | 3328 | 0.0552 |
| 2026-11-10 | fix-1 | deepseek-flash | no | 16072 | 13065 | 0 | 371840 | 0.0114 |
| 2026-11-10 | audit | deepseek-flash | no | 25396 | 20446 | 0 | 296192 | 0.0170 |
| 2026-11-10 | fix-2 | deepseek-flash | no | 9512 | 941 | 0 | 37632 | 0.0021 |
| 2026-11-10 | threat-model | deepseek-flash | no | 21138 | 13014 | 0 | 130688 | 0.0114 |
| 2026-11-10 | fix-3 | deepseek-flash | no | 9774 | 963 | 0 | 38400 | 0.0022 |
| 2026-11-10 | docs | deepseek-flash | no | 3615 | 11770 | 0 | 384 | 0.0076 |

Total: $0.1068. Prices: deepseek line, as measured by its adapter. Providers serve the current version of each model id, so the date tells which version answered; versions, launch stages and price sources per line are in the factory's model register (docs/MODELS.md).
