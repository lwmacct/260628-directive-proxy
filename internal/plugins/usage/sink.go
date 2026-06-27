package usage

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"

type Sink = eventbus.Publisher
type NopSink = eventbus.NopPublisher
