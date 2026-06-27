package proxydirective

import (
	"net/http"
	"strings"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type inputAdapter struct {
	extract func(*http.Request) (PayloadSource, bool)
}

type PayloadSource struct {
	Raw          string
	StripHeaders []string
	Source       string
}

type payloadResolver struct {
	adapters []inputAdapter
}

func NewResolver() proxyplan.Resolver {
	return &payloadResolver{
		adapters: []inputAdapter{
			{
				extract: func(req *http.Request) (PayloadSource, bool) {
					header := strings.TrimSpace(req.Header.Get("Authorization"))
					if !strings.HasPrefix(header, "Bearer ") {
						return PayloadSource{}, false
					}
					raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
					if raw == "" {
						return PayloadSource{}, false
					}
					if !strings.HasPrefix(raw, TokenPrefix) {
						return PayloadSource{}, false
					}
					return PayloadSource{
						Raw:          raw,
						StripHeaders: []string{"Authorization"},
						Source:       "authorization",
					}, true
				},
			},
		},
	}
}

func (r *payloadResolver) Resolve(req *http.Request) (*proxyplan.Plan, error) {
	for _, adapter := range r.adapters {
		if adapter.extract == nil {
			continue
		}
		source, ok := adapter.extract(req)
		if !ok {
			continue
		}
		plan, err := buildPlanFromSource(source)
		if err != nil {
			return nil, proxyplan.ErrInvalidDirective
		}
		return withRequestRuntime(plan, req), nil
	}
	return nil, proxyplan.ErrInvalidPlan
}

func buildPlanFromSource(source PayloadSource) (*proxyplan.Plan, error) {
	payload, err := Decode(source.Raw)
	if err != nil {
		return nil, err
	}
	return ToPlan(payload, AssembleOptions{
		StripHeaders: source.StripHeaders,
	})
}

func withRequestRuntime(plan *proxyplan.Plan, req *http.Request) *proxyplan.Plan {
	if plan == nil {
		return nil
	}
	cloned := *plan
	cloned.Labels = cloneLabels(plan.Labels)
	cloned.Runtime = eventbus.CloneRuntime(plan.Runtime)
	if req == nil {
		return &cloned
	}
	if remoteAddr := strings.TrimSpace(req.RemoteAddr); remoteAddr != "" {
		cloned.Runtime.IncomingRemoteAddr = remoteAddr
	}
	if clientRequestID := strings.TrimSpace(req.Header.Get(proxyplan.ClientRequestIDHeader)); clientRequestID != "" {
		cloned.Runtime.ClientRequestID = clientRequestID
	}
	runtimeHeaders := runtimeHeadersFromRequest(req)
	if len(runtimeHeaders) > 0 {
		cloned.Runtime.Headers = runtimeHeaders
	}
	return &cloned
}

func runtimeHeadersFromRequest(req *http.Request) map[string][]string {
	if req == nil || len(req.Header) == 0 {
		return nil
	}
	headers := make(map[string][]string)
	for name, values := range req.Header {
		canonicalName := http.CanonicalHeaderKey(strings.TrimSpace(name))
		if !strings.HasPrefix(strings.ToLower(canonicalName), "m-runtime-") {
			continue
		}
		headers[canonicalName] = append([]string(nil), values...)
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
