package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/requestid"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
)

type runtime struct {
	transport http.RoundTripper
	idGen     requestid.Generator
	proxy     *service.ProxyService
	tls       *tlsRuntime
}

func newRuntime(ctx context.Context, cfg *config.Config) (*runtime, error) {
	rt, err := newServiceRuntime(cfg)
	if err != nil {
		return nil, err
	}

	tlsRuntime, err := newTLSRuntime(ctx, cfg.Server.HTTP.TLS)
	if err != nil {
		_ = rt.Close(context.Background())
		return nil, fmt.Errorf("configure tls: %w", err)
	}
	rt.tls = tlsRuntime
	return rt, nil
}

func (rt *runtime) Close(_ context.Context) error {
	if rt == nil {
		return nil
	}
	if rt.tls != nil {
		rt.tls.Close()
		rt.tls = nil
	}
	return nil
}
