package directive

const (
	TokenFamily  = "dproxy"
	TokenVersion = "12"
	TokenInline  = "i"
	TokenRedis   = "r"
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
	Name   string   `json:"name,omitempty"`
	Glob   string   `json:"glob,omitempty"`
	Preset string   `json:"preset,omitempty"`
	Values []string `json:"values,omitempty"`
}
