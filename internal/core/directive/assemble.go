package directive

import (
	"net/url"
	"path"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type AssembleOptions struct {
	StripHeaders []string
}

func ToPlan(payload Payload, opts AssembleOptions) (*proxy.Plan, error) {
	targetURL := strings.TrimSpace(payload.Target.URL)
	if targetURL == "" {
		return nil, ErrInvalidPayload
	}
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" || !isHTTPURL(target) {
		return nil, ErrInvalidPayload
	}
	headerMode := ""
	var rawHeaderOps []HeaderOp
	if payload.Headers != nil {
		headerMode = payload.Headers.Mode
		rawHeaderOps = payload.Headers.Ops
	}
	if err := validateHeaderMode(headerMode); err != nil {
		return nil, err
	}
	proxyURL, err := ParseProxy(payload.Proxy)
	if err != nil {
		return nil, err
	}
	headerOps, metadata, err := parseHeaderOps(rawHeaderOps)
	if err != nil {
		return nil, err
	}
	ops := make([]proxy.HeaderOp, 0, len(opts.StripHeaders)+len(headerOps))
	for _, name := range opts.StripHeaders {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ops = append(ops, proxy.HeaderOp{
			Action: proxy.HeaderRemove,
			Selector: proxy.HeaderSelector{
				Kind:    proxy.HeaderSelectorExact,
				Pattern: name,
			},
		})
	}

	ops = append(ops, headerOps...)

	joinPath := true
	if payload.Target.JoinPath != nil {
		joinPath = *payload.Target.JoinPath
	}

	return &proxy.Plan{
		Target:     target,
		Proxy:      proxyURL,
		HeaderMode: toHeaderMode(headerMode),
		HeaderOps:  ops,
		Metadata:   metadata,
		JoinPath:   joinPath,
	}, nil
}

func isHTTPURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func parseHeaderOps(raw []HeaderOp) ([]proxy.HeaderOp, map[string][]string, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	ops := make([]proxy.HeaderOp, 0, len(raw))
	metadata := make(requestmeta.Metadata)
	for _, rawOp := range raw {
		actionRaw := strings.TrimSpace(rawOp.Op)
		action := proxy.HeaderAction(actionRaw)
		name := strings.TrimSpace(rawOp.Name)
		glob := strings.TrimSpace(rawOp.Glob)
		preset := strings.TrimSpace(rawOp.Preset)
		selectorCount := 0
		for _, value := range []string{name, glob, preset} {
			if value != "" {
				selectorCount++
			}
		}
		if selectorCount != 1 {
			return nil, nil, ErrInvalidPayload
		}
		selector := proxy.HeaderSelector{Kind: proxy.HeaderSelectorExact, Pattern: name}
		switch {
		case glob != "":
			if _, err := path.Match(strings.ToLower(glob), ""); err != nil {
				return nil, nil, ErrInvalidPayload
			}
			selector = proxy.HeaderSelector{Kind: proxy.HeaderSelectorGlob, Pattern: glob}
		case preset != "":
			if preset != proxy.HeaderPresetProxyDisclosure || action != proxy.HeaderRemove {
				return nil, nil, ErrInvalidPayload
			}
			selector = proxy.HeaderSelector{Kind: proxy.HeaderSelectorPreset, Pattern: preset}
		case !isValidHeaderName(name):
			return nil, nil, ErrInvalidPayload
		}
		switch action {
		case proxy.HeaderAdd, proxy.HeaderSet:
			if len(rawOp.Values) == 0 {
				return nil, nil, ErrInvalidPayload
			}
		case proxy.HeaderRemove:
			if len(rawOp.Values) != 0 {
				return nil, nil, ErrInvalidPayload
			}
		default:
			return nil, nil, ErrInvalidPayload
		}
		if selector.Kind == proxy.HeaderSelectorExact && strings.EqualFold(selector.Pattern, "Host") {
			if action == proxy.HeaderAdd || len(rawOp.Values) > 1 {
				return nil, nil, ErrInvalidPayload
			}
		}
		if selector.Kind == proxy.HeaderSelectorExact && requestmeta.IsName(selector.Pattern) {
			if err := requestmeta.Apply(metadata, string(action), selector.Pattern, rawOp.Values); err != nil {
				return nil, nil, ErrInvalidPayload
			}
			continue
		}
		ops = append(ops, proxy.HeaderOp{
			Action:   action,
			Selector: selector,
			Values:   append([]string(nil), rawOp.Values...),
		})
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return ops, metadata, nil
}

func isValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if !isHeaderTokenChar(char) {
			return false
		}
	}
	return true
}

func isHeaderTokenChar(char rune) bool {
	if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-.^_`|~", char)
}

func toHeaderMode(raw string) proxy.HeaderMode {
	switch proxy.HeaderMode(strings.TrimSpace(raw)) {
	case proxy.HeaderModeReplace:
		return proxy.HeaderModeReplace
	default:
		return proxy.HeaderModePatch
	}
}
