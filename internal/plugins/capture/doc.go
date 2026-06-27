// Package capture provides a reusable HTTP client transport that captures
// request, response, and streaming capture events when the request context carries
// an explicit proxyplan.CapturePolicy.
//
// The package emits unified eventbus envelopes and leaves routing or transport
// concerns to the eventbus package:
//
//   - package capture: HTTP capture transport and capture payloads
//   - package eventbus: envelope model, async buffering, publishers
//   - package eventbus/kafka: Kafka publisher
//
// A typical setup wraps a downstream publisher with eventbus.NewAsyncPublisher and then installs
// the transport on an http.Client.
//
// Example:
//
//	publisher := eventbus.NewAsyncPublisher(eventbus.NopPublisher{}, eventbus.AsyncOptions{})
//	client := &http.Client{
//		Transport: capture.NewTransport(http.DefaultTransport, publisher, capture.Options{}),
//	}
//	_ = client
package capture
