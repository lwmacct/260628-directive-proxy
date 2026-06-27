package capture

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"

type Event = eventbus.Event
type Sink = eventbus.Publisher
type NopSink = eventbus.NopPublisher
