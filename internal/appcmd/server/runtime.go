package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
)

type runtime struct {
	publisher     eventbus.Publisher
	usageDelivery eventbus.Publisher
	transport     http.RoundTripper
	idGen         eventbus.IDGenerator
	proxy         *service.ProxyService
	tls           *tlsRuntime
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

func (rt *runtime) Close(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	var errs []error
	if rt.tls != nil {
		rt.tls.Close()
		rt.tls = nil
	}
	if rt.usageDelivery != nil {
		if err := rt.usageDelivery.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		rt.usageDelivery = nil
	}
	if rt.publisher != nil {
		if err := rt.publisher.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		rt.publisher = nil
	}
	return errors.Join(errs...)
}
