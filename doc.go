// Package trafficmockrecorder is a small, dependency-free Go library: Record outbound HTTP traffic through a Go RoundTripper with redaction and caps, infer path templates from what was seen and serve the result as a mock http.Handler
//
// The public API is every exported identifier of this root package. Functions are pure: they do
// no I/O, keep no global mutable state and report invalid input through returned errors.
package trafficmockrecorder
