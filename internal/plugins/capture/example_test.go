package capture_test

import (
	"net/http"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/plugins/capture"
)

func ExampleNewTransport() {
	publisher := eventbus.NewAsyncPublisher(eventbus.NopPublisher{}, eventbus.AsyncOptions{})
	client := &http.Client{
		Transport: capture.NewTransport(http.DefaultTransport, publisher, capture.Options{}),
	}
	_ = client.Transport
}
