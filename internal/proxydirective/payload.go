package proxydirective

const (
	PayloadVersion = 1
	PayloadKind    = "directive-proxy.directive"
	TokenPrefix    = "dpx1."
)

type Payload struct {
	Version   int               `json:"version"`
	Kind      string            `json:"kind"`
	Target    TargetSection     `json:"target"`
	Transport *TransportSection `json:"transport,omitempty"`
	Headers   *HeaderSection    `json:"headers,omitempty"`
	Labels    map[string]any    `json:"labels,omitempty"`
}

type TargetSection struct {
	URL      string `json:"url"`
	JoinPath *bool  `json:"join_path,omitempty"`
}

type TransportSection struct {
	Proxy string `json:"proxy,omitempty"`
}

type HeaderSection struct {
	Mode string     `json:"mode,omitempty"`
	Ops  []HeaderOp `json:"ops,omitempty"`
}

type HeaderOp struct {
	Op     string   `json:"op"`
	Name   string   `json:"name"`
	Values []string `json:"values,omitempty"`
}
