package usage

import (
	"encoding/json"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
)

const EventTypeUsage = eventbus.TypeUsage

type Data map[string]json.RawMessage
