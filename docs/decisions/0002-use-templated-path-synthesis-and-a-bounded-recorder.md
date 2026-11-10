# Use templated path synthesis and a bounded recorder

Status: accepted (generated, pending human review)

## Context

Integration tests need to record real HTTP exchanges and replay them without breaking when IDs change. Exact cassettes are brittle.

## Decision

Store exchanges in a ring buffer with size and body limits; redact sensitive query parameters and response headers; synthesize path templates by generalizing digit and UUID segments; serve latest response per template via an http.Handler.

## Consequences

The library is safe for concurrent use, memory usage is bounded, and replays are resilient to identifier changes. It uses only the standard library, simplifying deployment.
