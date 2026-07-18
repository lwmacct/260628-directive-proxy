package directive

import (
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

func resolveRequest(resolver proxy.Resolver, req *http.Request) (*proxy.PreparedDirective, error) {
	prepared, err := resolver.Prepare(req)
	if err != nil {
		return nil, err
	}
	return prepared, nil
}
