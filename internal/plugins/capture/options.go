package capture

import (
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type Options struct {
	IDGenerator           eventbus.IDGenerator
	AbnormalCapture       bool
	AbnormalCapturePolicy proxyplan.CapturePolicy
}

func (o Options) withDefaults() Options {
	if o.IDGenerator == nil {
		o.IDGenerator = eventbus.NewIDGenerator()
	}
	if o.AbnormalCapture && !o.AbnormalCapturePolicy.Configured {
		o.AbnormalCapturePolicy = proxyplan.CapturePolicy{
			Configured:       true,
			RequestHeaders:   true,
			RequestBody:      true,
			ResponseHeaders:  true,
			ResponseBody:     true,
			StreamEvents:     false,
			StreamEventTypes: nil,
		}
	}
	o.AbnormalCapturePolicy = o.AbnormalCapturePolicy.WithDefaults()
	return o
}
