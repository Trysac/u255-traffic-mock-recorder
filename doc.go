// Package trafficmockrecorder records outbound HTTP traffic with redaction and
// caps, infers path templates, and serves recorded responses as a mock handler.
//
// The Recorder wraps an http.RoundTripper to capture exchanges. Synthesize
// groups exchanges into stubs with templated paths. NewMockHandler serves those
// stubs for testing.
package trafficmockrecorder
