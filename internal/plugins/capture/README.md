# `capture`

`capture` captures proxied HTTP request, response, parsed stream event, and stream end payloads and publishes them as unified eventbus envelopes.

## Responsibilities

- publish request and response capture events through an `eventbus.Publisher`
- capture headers, bodies, and parsed SSE events according to the directive capture policy
- attach directive labels and runtime context to emitted event envelopes

Capture is disabled unless the resolved directive contains an explicit capture
policy. A payload with no `capture` field produces no request/response/stream
capture events.

Prometheus metrics are intentionally not included in `directive-proxy`.
