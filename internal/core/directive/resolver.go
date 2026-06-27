package directive

import (
	"net/http"
	"strings"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

type Resolver struct{}

func NewResolver() proxy.Resolver {
	return Resolver{}
}

func (Resolver) Resolve(req *http.Request) (*proxy.Plan, error) {
	raw, ok := directiveTokenFromAuthorization(req)
	if !ok {
		return nil, proxy.ErrInvalidPlan
	}
	payload, err := Decode(raw)
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}
	plan, err := ToPlan(payload, AssembleOptions{
		StripHeaders: []string{"Authorization"},
	})
	if err != nil {
		return nil, proxy.ErrInvalidDirective
	}
	return plan, nil
}

func directiveTokenFromAuthorization(req *http.Request) (string, bool) {
	if req == nil {
		return "", false
	}
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if raw == "" || !strings.HasPrefix(raw, TokenPrefix) {
		return "", false
	}
	return raw, true
}
