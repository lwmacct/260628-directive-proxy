package httpheader

type Mode string

const (
	ModePatch   Mode = "patch"
	ModeReplace Mode = "replace"
)

type Plan struct {
	Request  RequestPlan
	Response ResponsePlan
}

type RequestPlan struct {
	Mode                    Mode
	PreserveProxyDisclosure bool
	StripBeforeOps          []string
	Ops                     []Op
}

type ResponsePlan struct {
	Ops []Op
}

type Action string

const (
	ActionAdd    Action = "+"
	ActionRemove Action = "-"
	ActionSet    Action = "="
)

type SelectorKind string

const (
	SelectorExact SelectorKind = "exact"
	SelectorGlob  SelectorKind = "glob"
)

type Selector struct {
	Kind    SelectorKind
	Pattern string
}

type Op struct {
	Action   Action
	Selector Selector
	Values   []string
}

func ClonePlan(in Plan) Plan {
	out := in
	out.Request.StripBeforeOps = append([]string(nil), in.Request.StripBeforeOps...)
	out.Request.Ops = CloneOps(in.Request.Ops)
	out.Response.Ops = CloneOps(in.Response.Ops)
	return out
}

func CloneOps(in []Op) []Op {
	out := make([]Op, len(in))
	for index, op := range in {
		out[index] = op
		out[index].Values = append([]string(nil), op.Values...)
	}
	return out
}
