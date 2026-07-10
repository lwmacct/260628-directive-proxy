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

// IsDirectiveRequest reports whether the request carries a token in the
// dproxy Authorization namespace. Token version and payload validation are
// intentionally left to Resolver.
func IsDirectiveRequest(req *http.Request) bool {
	_, ok := directiveTokenFromAuthorization(req)
	return ok
}

func directiveTokenFromAuthorization(req *http.Request) (string, bool) {
	if req == nil {
		return "", false
	}
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	raw := parts[1]
	if !strings.HasPrefix(raw, TokenFamily+".") {
		return "", false
	}
	return raw, true
}
