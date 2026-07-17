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

const (
	maxModuleSpecs     = 16
	maxModuleNameBytes = 64
	maxModuleSpecBytes = 64 << 10
)

func ToPlan(payload Payload, opts AssembleOptions) (*proxy.Plan, error) {
	targetURL := strings.TrimSpace(payload.Target.URL)
	if targetURL == "" {
		return nil, ErrInvalidPayload
	}
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme == "" || target.Host == "" || !isHTTPURL(target) {
		return nil, ErrInvalidPayload
	}
	headers := HeaderPolicy{}
	if payload.Headers != nil {
		headers = *payload.Headers
	}
	if err := validateHeaderMode(headers.Mode); err != nil {
		return nil, err
	}
	proxyURL, err := ParseProxy(payload.Proxy)
	if err != nil {
		return nil, err
	}
	requestRaw, responseRaw, err := splitHeaderMutations(headers.Mutations, true)
	if err != nil {
		return nil, err
	}
	requestOps, metadata, err := parseRequestHeaderMutations(requestRaw)
	if err != nil {
		return nil, err
	}
	responseOps, err := parseResponseHeaderMutations(responseRaw)
	if err != nil {
		return nil, err
	}
	program, err := normalizeProgram(payload.Program, true, true)
	if err != nil {
		return nil, err
	}
	recoveryPolicy, err := CompileRecovery(payload.Recovery)
	if err != nil {
		return nil, err
	}
	stripBeforeOps := make([]string, 0, len(opts.StripHeaders))
	for _, name := range opts.StripHeaders {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		stripBeforeOps = append(stripBeforeOps, name)
	}

	joinPath := true
	if payload.Target.JoinPath != nil {
		joinPath = *payload.Target.JoinPath
	}

	return &proxy.Plan{
		Target: target,
		Proxy:  proxyURL,
		Headers: proxy.HeaderPlan{
			Request: proxy.RequestHeaderPlan{
				Mode:                    toHeaderMode(headers.Mode),
				PreserveProxyDisclosure: headers.PreserveProxyDisclosure,
				StripBeforeOps:          stripBeforeOps,
				Ops:                     requestOps,
			},
			Response: proxy.ResponseHeaderPlan{Ops: responseOps},
		},
		Metadata: metadata,
		Modules:  program.Attempt,
		Recovery: recoveryPolicy,
		JoinPath: joinPath,
	}, nil
}

func isModuleName(value string) bool {
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || (char == '-' || char == '.') && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}

func isHTTPURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func parseRequestHeaderMutations(raw []HeaderMutation) ([]proxy.HeaderOp, map[string][]string, error) {
	ops, err := parseHeaderMutations(raw)
	if err != nil {
		return nil, nil, err
	}
	out := make([]proxy.HeaderOp, 0, len(ops))
	metadata := make(requestmeta.Metadata)
	for _, op := range ops {
		if op.Selector.Kind == proxy.HeaderSelectorExact && strings.EqualFold(op.Selector.Pattern, "Host") {
			if op.Action == proxy.HeaderAdd || len(op.Values) > 1 {
				return nil, nil, ErrInvalidPayload
			}
		}
		if op.Selector.Kind == proxy.HeaderSelectorExact && requestmeta.IsName(op.Selector.Pattern) {
			if err := requestmeta.Apply(metadata, string(op.Action), op.Selector.Pattern, op.Values); err != nil {
				return nil, nil, ErrInvalidPayload
			}
			continue
		}
		out = append(out, op)
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return out, metadata, nil
}

// CompileResolverRequestHeaders compiles the direct HTTP request header policy
// used by an HTTP RemoteSpec. It intentionally does not extract x-dproxy
// metadata; the resolver request applies the same header mutations directly.
func CompileResolverRequestHeaders(section *HeaderPolicy) (proxy.RequestHeaderPlan, error) {
	if section == nil {
		section = &HeaderPolicy{}
	}
	if err := validateHeaderMode(section.Mode); err != nil {
		return proxy.RequestHeaderPlan{}, err
	}
	requestRaw, responseRaw, err := splitHeaderMutations(section.Mutations, false)
	if err != nil || len(responseRaw) > 0 {
		return proxy.RequestHeaderPlan{}, ErrInvalidPayload
	}
	ops, err := parseHeaderMutations(requestRaw)
	if err != nil {
		return proxy.RequestHeaderPlan{}, err
	}
	for _, op := range ops {
		if op.Selector.Kind == proxy.HeaderSelectorExact && strings.EqualFold(op.Selector.Pattern, "Host") &&
			(op.Action == proxy.HeaderAdd || len(op.Values) > 1) {
			return proxy.RequestHeaderPlan{}, ErrInvalidPayload
		}
	}
	return proxy.RequestHeaderPlan{
		Mode:                    toHeaderMode(section.Mode),
		PreserveProxyDisclosure: section.PreserveProxyDisclosure,
		StripBeforeOps:          []string{"Authorization", "Content-Length"},
		Ops:                     ops,
	}, nil
}

func splitHeaderMutations(raw []HeaderMutation, allowResponse bool) ([]HeaderMutation, []HeaderMutation, error) {
	request := make([]HeaderMutation, 0, len(raw))
	response := make([]HeaderMutation, 0, len(raw))
	for _, mutation := range raw {
		switch mutation.Side {
		case HeaderSideRequest:
			request = append(request, mutation)
		case HeaderSideResponse:
			if !allowResponse {
				return nil, nil, ErrInvalidPayload
			}
			response = append(response, mutation)
		default:
			return nil, nil, ErrInvalidPayload
		}
	}
	return request, response, nil
}

func parseResponseHeaderMutations(raw []HeaderMutation) ([]proxy.HeaderOp, error) {
	ops, err := parseHeaderMutations(raw)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.Selector.Kind == proxy.HeaderSelectorExact && proxy.IsResponseHeaderProtected(op.Selector.Pattern) {
			return nil, ErrInvalidPayload
		}
	}
	return ops, nil
}

func parseHeaderMutations(raw []HeaderMutation) ([]proxy.HeaderOp, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ops := make([]proxy.HeaderOp, 0, len(raw))
	for _, mutation := range raw {
		var action proxy.HeaderAction
		switch mutation.Action {
		case HeaderActionSet:
			action = proxy.HeaderSet
		case HeaderActionRemove:
			action = proxy.HeaderRemove
		case HeaderActionAppend:
			action = proxy.HeaderAdd
		default:
			return nil, ErrInvalidPayload
		}
		name := strings.TrimSpace(mutation.Name)
		glob := strings.TrimSpace(mutation.Glob)
		if (name == "") == (glob == "") {
			return nil, ErrInvalidPayload
		}
		selector := proxy.HeaderSelector{Kind: proxy.HeaderSelectorExact, Pattern: name}
		if glob != "" {
			if _, err := path.Match(strings.ToLower(glob), ""); err != nil {
				return nil, ErrInvalidPayload
			}
			selector = proxy.HeaderSelector{Kind: proxy.HeaderSelectorGlob, Pattern: glob}
		} else if !isValidHeaderName(name) {
			return nil, ErrInvalidPayload
		}
		switch action {
		case proxy.HeaderAdd, proxy.HeaderSet:
			if len(mutation.Values) == 0 {
				return nil, ErrInvalidPayload
			}
			for _, value := range mutation.Values {
				if !isValidHeaderValue(value) {
					return nil, ErrInvalidPayload
				}
			}
		case proxy.HeaderRemove:
			if len(mutation.Values) != 0 {
				return nil, ErrInvalidPayload
			}
		}
		ops = append(ops, proxy.HeaderOp{
			Action:   action,
			Selector: selector,
			Values:   append([]string(nil), mutation.Values...),
		})
	}
	return ops, nil
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

func isValidHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '\t' || char >= 0x20 && char != 0x7f {
			continue
		}
		return false
	}
	return true
}

func toHeaderMode(raw string) proxy.HeaderMode {
	switch proxy.HeaderMode(strings.TrimSpace(raw)) {
	case proxy.HeaderModeReplace:
		return proxy.HeaderModeReplace
	default:
		return proxy.HeaderModePatch
	}
}
