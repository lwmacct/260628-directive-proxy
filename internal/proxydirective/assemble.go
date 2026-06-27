package proxydirective

import (
	"net/url"
	"strings"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type AssembleOptions struct {
	StripHeaders []string
}

type NormalizedPayload struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode proxyplan.HeaderMode
	HeaderOps  []proxyplan.HeaderOp
	Labels     map[string]any
	JoinPath   bool
}

func ToPlan(payload Payload, opts AssembleOptions) (*proxyplan.Plan, error) {
	normalized, err := NormalizePayload(payload, opts)
	if err != nil {
		return nil, err
	}
	return BuildPlan(normalized), nil
}

func NormalizePayload(payload Payload, opts AssembleOptions) (NormalizedPayload, error) {
	if payload.Version != PayloadVersion || strings.TrimSpace(payload.Kind) != PayloadKind {
		return NormalizedPayload{}, ErrInvalidPayload
	}
	targetURL := strings.TrimSpace(payload.Target.URL)
	if targetURL == "" {
		return NormalizedPayload{}, ErrInvalidPayload
	}
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" || !isHTTPURL(target) {
		return NormalizedPayload{}, ErrInvalidPayload
	}
	headerMode := ""
	var rawHeaderOps []HeaderOp
	if payload.Headers != nil {
		headerMode = payload.Headers.Mode
		rawHeaderOps = payload.Headers.Ops
	}
	if err := validateHeaderMode(headerMode); err != nil {
		return NormalizedPayload{}, err
	}
	if err := validateLabels(payload.Labels); err != nil {
		return NormalizedPayload{}, ErrInvalidPayload
	}
	proxyRaw := ""
	if payload.Transport != nil {
		proxyRaw = payload.Transport.Proxy
	}
	proxyURL, err := ParseProxy(proxyRaw)
	if err != nil {
		return NormalizedPayload{}, err
	}
	headerOps, err := parseHeaderOps(rawHeaderOps)
	if err != nil {
		return NormalizedPayload{}, err
	}
	ops := make([]proxyplan.HeaderOp, 0, len(opts.StripHeaders)+len(headerOps))
	for _, name := range opts.StripHeaders {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ops = append(ops, proxyplan.HeaderOp{
			Action: proxyplan.HeaderRemove,
			Name:   name,
		})
	}

	ops = append(ops, headerOps...)

	joinPath := true
	if payload.Target.JoinPath != nil {
		joinPath = *payload.Target.JoinPath
	}

	return NormalizedPayload{
		Target:     target,
		Proxy:      proxyURL,
		HeaderMode: toHeaderMode(headerMode),
		HeaderOps:  ops,
		Labels:     cloneLabels(payload.Labels),
		JoinPath:   joinPath,
	}, nil
}

func isHTTPURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func BuildPlan(payload NormalizedPayload) *proxyplan.Plan {
	return &proxyplan.Plan{
		Target:     payload.Target,
		Proxy:      payload.Proxy,
		HeaderMode: payload.HeaderMode,
		HeaderOps:  append([]proxyplan.HeaderOp(nil), payload.HeaderOps...),
		Labels:     cloneLabels(payload.Labels),
		JoinPath:   payload.JoinPath,
	}
}

func parseHeaderOps(raw []HeaderOp) ([]proxyplan.HeaderOp, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ops := make([]proxyplan.HeaderOp, 0, len(raw))
	for _, rawOp := range raw {
		actionRaw := strings.TrimSpace(rawOp.Op)
		action := proxyplan.HeaderAction(actionRaw)
		name := strings.TrimSpace(rawOp.Name)
		if name == "" {
			return nil, ErrInvalidPayload
		}
		switch action {
		case proxyplan.HeaderAdd, proxyplan.HeaderSet:
			if len(rawOp.Values) == 0 {
				return nil, ErrInvalidPayload
			}
		case proxyplan.HeaderRemove:
		default:
			return nil, ErrInvalidPayload
		}
		if strings.EqualFold(name, "Host") {
			if action == proxyplan.HeaderAdd || len(rawOp.Values) > 1 {
				return nil, ErrInvalidPayload
			}
		}
		ops = append(ops, proxyplan.HeaderOp{
			Action: action,
			Name:   name,
			Values: append([]string(nil), rawOp.Values...),
		})
	}
	return ops, nil
}

func toHeaderMode(raw string) proxyplan.HeaderMode {
	switch proxyplan.HeaderMode(strings.TrimSpace(raw)) {
	case proxyplan.HeaderModeReplace:
		return proxyplan.HeaderModeReplace
	default:
		return proxyplan.HeaderModePatch
	}
}

func cloneLabels(labels map[string]any) map[string]any {
	if len(labels) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}
