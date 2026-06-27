package proxydirective

import (
	"net/http"
	"strings"

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
		return plan, nil
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
