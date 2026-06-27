package server

import (
	"context"
	"crypto/tls"
	"log/slog"
	"time"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
)

const httpTLSMinVersion = tls.VersionTLS12

type tlsRuntime struct {
	config   *tls.Config
	reloader *tlsreload.Reloader
}

func newTLSRuntime(ctx context.Context, cfg config.ServerHTTPTLS) (*tlsRuntime, error) {
	if !cfg.Enabled {
		return &tlsRuntime{}, nil
	}

	if !cfg.AutoReload {
		certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		return &tlsRuntime{
			config: &tls.Config{
				Certificates: []tls.Certificate{certificate},
				MinVersion:   httpTLSMinVersion,
			},
		}, nil
	}

	reloader, err := tlsreload.New(ctx, tlsreload.Config{
		CertFile:       cfg.CertFile,
		KeyFile:        cfg.KeyFile,
		ReloadInterval: cfg.ReloadInterval,
		RetryInterval:  2 * time.Second,
		MinVersion:     httpTLSMinVersion,
		Logger:         slog.Default(),
	})
	if err != nil {
		return nil, err
	}

	return &tlsRuntime{
		config:   reloader.TLSConfig(),
		reloader: reloader,
	}, nil
}

func (rt *tlsRuntime) Close() {
	if rt == nil || rt.reloader == nil {
		return
	}
	rt.reloader.Close()
	rt.reloader = nil
}
