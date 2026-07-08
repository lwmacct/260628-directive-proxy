package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
)

type App struct {
	cfg *config.Config
}

func NewApp(cfg *config.Config) *App {
	return &App{cfg: cfg}
}

func (app *App) Run(ctx context.Context) error {
	if err := validateConfig(app.cfg); err != nil {
		return err
	}

	rt, err := newRuntime(ctx, app.cfg)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close(context.Background()) }()

	controlSrv, err := newControlHTTPServer(app.cfg, rt)
	if err != nil {
		return err
	}
	controlLn, err := (&net.ListenConfig{}).Listen(ctx, "tcp", controlSrv.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = controlLn.Close() }()

	proxySrv, err := newProxyHTTPServer(app.cfg, rt)
	if err != nil {
		return err
	}
	proxyLn, err := (&net.ListenConfig{}).Listen(ctx, "tcp", proxySrv.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = proxyLn.Close() }()

	errCh := make(chan error, 2)
	startHTTPServer := func(name string, srv *http.Server, ln net.Listener) {
		cfg := app.cfg.Server.HTTP
		slog.Info("http server starting", "plane", name, "listen", srv.Addr, "https", cfg.TLS.Enabled)
		var serveErr error
		if cfg.TLS.Enabled {
			serveErr = srv.ServeTLS(ln, "", "")
		} else {
			serveErr = srv.Serve(ln)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("http server failed", "plane", name, "error", serveErr)
			errCh <- serveErr
		}
	}
	go startHTTPServer("control", controlSrv, controlLn)
	go startHTTPServer("proxy", proxySrv, proxyLn)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		return shutdown(ctx, []*http.Server{controlSrv, proxySrv}, rt)
	case sig := <-sigCh:
		slog.Info("received shutdown signal", "signal", sig.String())
		return shutdown(ctx, []*http.Server{controlSrv, proxySrv}, rt)
	case err := <-errCh:
		return err
	}
}

func validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return config.ErrInvalidHTTP
	}
	validated, err := config.Validate(*cfg)
	if err != nil {
		return err
	}
	*cfg = validated
	return nil
}

func shutdown(ctx context.Context, servers []*http.Server, rt *runtime) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var errs []error
	for _, srv := range servers {
		if srv == nil {
			continue
		}
		if err := srv.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := rt.Close(shutdownCtx); err != nil {
		errs = append(errs, err)
	}
	slog.Info("directive proxy service stopped")
	return errors.Join(errs...)
}
