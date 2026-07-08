package directive

const (
	TokenFamily  = "dproxy"
	TokenVersion = "10"
)

type Payload struct {
	Target  TargetSection  `json:"target"`
	Proxy   string         `json:"proxy,omitempty"`
	Headers *HeaderSection `json:"headers,omitempty"`
}

type TargetSection struct {
	URL      string `json:"url"`
	JoinPath *bool  `json:"join_path,omitempty"`
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
