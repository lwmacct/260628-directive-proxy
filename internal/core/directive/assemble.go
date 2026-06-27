package directive

import (
	"net/url"
	"strings"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

type AssembleOptions struct {
	StripHeaders []string
}

type NormalizedPayload struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode proxy.HeaderMode
	HeaderOps  []proxy.HeaderOp
	JoinPath   bool
}

func ToPlan(payload Payload, opts AssembleOptions) (*proxy.Plan, error) {
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
	ops := make([]proxy.HeaderOp, 0, len(opts.StripHeaders)+len(headerOps))
	for _, name := range opts.StripHeaders {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ops = append(ops, proxy.HeaderOp{
			Action: proxy.HeaderRemove,
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
		JoinPath:   joinPath,
	}, nil
}

func isHTTPURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func BuildPlan(payload NormalizedPayload) *proxy.Plan {
	return &proxy.Plan{
		Target:     payload.Target,
		Proxy:      payload.Proxy,
		HeaderMode: payload.HeaderMode,
		HeaderOps:  append([]proxy.HeaderOp(nil), payload.HeaderOps...),
		JoinPath:   payload.JoinPath,
	}
}

func parseHeaderOps(raw []HeaderOp) ([]proxy.HeaderOp, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ops := make([]proxy.HeaderOp, 0, len(raw))
	for _, rawOp := range raw {
		actionRaw := strings.TrimSpace(rawOp.Op)
		action := proxy.HeaderAction(actionRaw)
		name := strings.TrimSpace(rawOp.Name)
		if name == "" {
			return nil, ErrInvalidPayload
		}
		switch action {
		case proxy.HeaderAdd, proxy.HeaderSet:
			if len(rawOp.Values) == 0 {
				return nil, ErrInvalidPayload
			}
		case proxy.HeaderRemove:
		default:
			return nil, ErrInvalidPayload
		}
		if strings.EqualFold(name, "Host") {
			if action == proxy.HeaderAdd || len(rawOp.Values) > 1 {
				return nil, ErrInvalidPayload
			}
		}
		ops = append(ops, proxy.HeaderOp{
			Action: action,
			Name:   name,
			Values: append([]string(nil), rawOp.Values...),
		})
	}
	return ops, nil
}

func toHeaderMode(raw string) proxy.HeaderMode {
	switch proxy.HeaderMode(strings.TrimSpace(raw)) {
	case proxy.HeaderModeReplace:
		return proxy.HeaderModeReplace
	default:
		return proxy.HeaderModePatch
	}
}
