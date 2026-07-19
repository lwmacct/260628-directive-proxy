package directive

import (
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

type AssembleOptions struct {
	StripHeaders     []string
	InboundURL       *url.URL
	RecoveryCompiler recovery.Compiler
}

const (
	maxModuleSpecs     = 16
	maxModuleNameBytes = 64
	maxModuleSpecBytes = 64 << 10
)

type CompiledPayload struct {
	Plan      *proxy.Plan
	Metadata  metadata.Set
	Recovery  *recovery.Policy
	BodyStore *proxy.BodyPolicy
}

func CompilePayload(payload Payload, opts AssembleOptions) (CompiledPayload, error) {
	compiledMetadata, err := metadata.Compile(payload.Metadata)
	if err != nil {
		return CompiledPayload{}, ErrInvalidPayload
	}
	target, err := compileTarget(payload.Target, opts.InboundURL)
	if err != nil {
		return CompiledPayload{}, ErrInvalidPayload
	}
	headers := HeaderPolicy{}
	if payload.Headers != nil {
		headers = *payload.Headers
	}
	proxyURL, err := ParseProxy(payload.Proxy)
	if err != nil {
		return CompiledPayload{}, err
	}
	requestRaw, responseRaw, err := splitHeaderMutations(headers.Mutations, true)
	if err != nil {
		return CompiledPayload{}, err
	}
	requestOps, err := parseRequestHeaderMutations(requestRaw)
	if err != nil {
		return CompiledPayload{}, err
	}
	responseOps, err := parseResponseHeaderMutations(responseRaw)
	if err != nil {
		return CompiledPayload{}, err
	}
	if _, err := normalizeModules(payload.Modules); err != nil {
		return CompiledPayload{}, err
	}
	recoveryPolicy, err := CompileRecovery(payload.Recovery, opts.RecoveryCompiler)
	if err != nil {
		return CompiledPayload{}, err
	}
	bodyPolicy, err := compileBodyStore(payload.BodyStore)
	if err != nil {
		return CompiledPayload{}, err
	}
	stripBeforeOps := make([]string, 0, len(opts.StripHeaders))
	for _, name := range opts.StripHeaders {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		stripBeforeOps = append(stripBeforeOps, name)
	}

	return CompiledPayload{
		Plan: &proxy.Plan{
			Target: target,
			Proxy:  proxyURL,
			Headers: httpheader.Plan{
				Request: httpheader.RequestPlan{
					PreserveProxyDisclosure: headers.PreserveProxyDisclosure,
					StripBeforeOps:          stripBeforeOps,
					Ops:                     requestOps,
				},
				Response: httpheader.ResponsePlan{Ops: responseOps},
			},
		},
		Metadata:  compiledMetadata,
		Recovery:  recoveryPolicy,
		BodyStore: bodyPolicy,
	}, nil
}

func compileBodyStore(spec *BodyStoreSpec) (*proxy.BodyPolicy, error) {
	if spec == nil {
		return nil, nil
	}
	if spec.MaxBodyBytes != nil && *spec.MaxBodyBytes < 0 || spec.ChunkBytes != nil && *spec.ChunkBytes < 0 {
		return nil, ErrInvalidPayload
	}
	if spec.MaxBodyBytes != nil && *spec.MaxBodyBytes == 0 || spec.ChunkBytes != nil && *spec.ChunkBytes == 0 {
		return nil, ErrInvalidPayload
	}
	queueWait, err := parseBodyDuration(spec.QueueWait)
	if err != nil {
		return nil, err
	}
	readTimeout, err := parseBodyDuration(spec.ReadTimeout)
	if err != nil {
		return nil, err
	}
	if spec.ChunkBytes != nil && *spec.ChunkBytes > 0 && (*spec.ChunkBytes < 4<<10 || *spec.ChunkBytes > 1<<20) {
		return nil, ErrInvalidPayload
	}
	if spec.MaxBodyBytes == nil && spec.QueueWait == nil && spec.ReadTimeout == nil && spec.ChunkBytes == nil {
		return nil, nil
	}
	policy := &proxy.BodyPolicy{MaxBodyBytes: -1, QueueWait: -1, ReadTimeout: -1, ChunkBytes: -1}
	if spec.MaxBodyBytes != nil {
		policy.MaxBodyBytes = *spec.MaxBodyBytes
	}
	if spec.QueueWait != nil {
		policy.QueueWait = queueWait
	}
	if spec.ReadTimeout != nil {
		policy.ReadTimeout = readTimeout
	}
	if spec.ChunkBytes != nil {
		policy.ChunkBytes = *spec.ChunkBytes
	}
	return policy, nil
}

func parseBodyDuration(raw *string) (time.Duration, error) {
	if raw == nil {
		return 0, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return 0, ErrInvalidPayload
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, ErrInvalidPayload
	}
	return duration, nil
}

func compileTarget(section TargetSection, inbound *url.URL) (*url.URL, error) {
	baseURL := strings.TrimSpace(section.BaseURL)
	exactURL := strings.TrimSpace(section.ExactURL)
	if (baseURL == "") == (exactURL == "") {
		return nil, ErrInvalidPayload
	}
	raw := exactURL
	if baseURL != "" {
		raw = baseURL
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme == "" || target.Host == "" || !isHTTPURL(target) {
		return nil, ErrInvalidPayload
	}
	if baseURL == "" || inbound == nil {
		return target, nil
	}
	target.RawQuery = joinRawQuery(target.RawQuery, inbound.RawQuery)
	target.Path, target.RawPath = joinURLPath(target, inbound)
	return target, nil
}

func joinURLPath(base, inbound *url.URL) (string, string) {
	if base.RawPath == "" && inbound.RawPath == "" {
		return singleJoiningSlash(base.Path, inbound.Path), ""
	}
	basePath := base.EscapedPath()
	inboundPath := inbound.EscapedPath()
	baseSlash := strings.HasSuffix(basePath, "/")
	inboundSlash := strings.HasPrefix(inboundPath, "/")
	switch {
	case baseSlash && inboundSlash:
		return base.Path + inbound.Path[1:], basePath + inboundPath[1:]
	case !baseSlash && !inboundSlash:
		return base.Path + "/" + inbound.Path, basePath + "/" + inboundPath
	default:
		return base.Path + inbound.Path, basePath + inboundPath
	}
}

func singleJoiningSlash(left, right string) string {
	leftSlash := strings.HasSuffix(left, "/")
	rightSlash := strings.HasPrefix(right, "/")
	switch {
	case leftSlash && rightSlash:
		return left + right[1:]
	case !leftSlash && !rightSlash:
		return left + "/" + right
	default:
		return left + right
	}
}

func joinRawQuery(baseQuery, inboundQuery string) string {
	switch {
	case baseQuery == "":
		return inboundQuery
	case inboundQuery == "":
		return baseQuery
	default:
		return baseQuery + "&" + inboundQuery
	}
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

func parseRequestHeaderMutations(raw []HeaderMutation) ([]httpheader.Op, error) {
	ops, err := parseHeaderMutations(raw)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.Selector.Kind == httpheader.SelectorExact && strings.EqualFold(op.Selector.Pattern, "Host") {
			if op.Action == httpheader.ActionAdd || len(op.Values) > 1 {
				return nil, ErrInvalidPayload
			}
		}
		if op.Selector.Kind == httpheader.SelectorExact && httpheader.IsSystemHeader(op.Selector.Pattern) {
			return nil, ErrInvalidPayload
		}
	}
	return ops, nil
}

// CompileResolverRequestHeaders compiles the direct HTTP request header policy
// used by an HTTP RemoteSpec. It intentionally does not extract metadata from
// system headers; the resolver request applies header mutations directly.
func CompileResolverRequestHeaders(section *HeaderPolicy) (httpheader.RequestPlan, error) {
	if section == nil {
		section = &HeaderPolicy{}
	}
	requestRaw, responseRaw, err := splitHeaderMutations(section.Mutations, false)
	if err != nil || len(responseRaw) > 0 {
		return httpheader.RequestPlan{}, ErrInvalidPayload
	}
	ops, err := parseHeaderMutations(requestRaw)
	if err != nil {
		return httpheader.RequestPlan{}, err
	}
	for _, op := range ops {
		if op.Selector.Kind == httpheader.SelectorExact && httpheader.IsSystemHeader(op.Selector.Pattern) {
			return httpheader.RequestPlan{}, ErrInvalidPayload
		}
		if op.Selector.Kind == httpheader.SelectorExact && strings.EqualFold(op.Selector.Pattern, "Host") &&
			(op.Action == httpheader.ActionAdd || len(op.Values) > 1) {
			return httpheader.RequestPlan{}, ErrInvalidPayload
		}
	}
	return httpheader.RequestPlan{
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

func parseResponseHeaderMutations(raw []HeaderMutation) ([]httpheader.Op, error) {
	ops, err := parseHeaderMutations(raw)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.Selector.Kind == httpheader.SelectorExact && proxy.IsResponseHeaderProtected(op.Selector.Pattern) {
			return nil, ErrInvalidPayload
		}
	}
	return ops, nil
}

func parseHeaderMutations(raw []HeaderMutation) ([]httpheader.Op, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ops := make([]httpheader.Op, 0, len(raw))
	for _, mutation := range raw {
		var action httpheader.Action
		switch mutation.Action {
		case HeaderActionSet:
			action = httpheader.ActionSet
		case HeaderActionDel:
			action = httpheader.ActionDel
		case HeaderActionAdd:
			action = httpheader.ActionAdd
		default:
			return nil, ErrInvalidPayload
		}
		name := strings.TrimSpace(mutation.Name)
		glob := strings.TrimSpace(mutation.Glob)
		if (name == "") == (glob == "") {
			return nil, ErrInvalidPayload
		}
		selector := httpheader.Selector{Kind: httpheader.SelectorExact, Pattern: name}
		if glob != "" {
			if _, err := path.Match(strings.ToLower(glob), ""); err != nil {
				return nil, ErrInvalidPayload
			}
			selector = httpheader.Selector{Kind: httpheader.SelectorGlob, Pattern: glob}
		} else if !isValidHeaderName(name) {
			return nil, ErrInvalidPayload
		}
		switch action {
		case httpheader.ActionAdd:
			if len(mutation.Values) == 0 {
				return nil, ErrInvalidPayload
			}
			for _, value := range mutation.Values {
				if !isValidHeaderValue(value) {
					return nil, ErrInvalidPayload
				}
			}
		case httpheader.ActionSet:
			if len(mutation.Values) != 1 || !isValidHeaderValue(mutation.Values[0]) {
				return nil, ErrInvalidPayload
			}
		case httpheader.ActionDel:
			if len(mutation.Values) != 0 {
				return nil, ErrInvalidPayload
			}
		}
		ops = append(ops, httpheader.Op{
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
