package directive

import (
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

func resolveRequest(resolver proxy.Resolver, req *http.Request) (proxy.Resolution, error) {
	prepared, err := resolver.Prepare(req)
	if err != nil {
		return proxy.Resolution{}, err
	}
	return prepared.ResolveAttempt(req.Context(), 1)
}
