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

	srv, err := newHTTPServer(app.cfg, rt)
	if err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	errCh := make(chan error, 1)
	go func() {
		cfg := app.cfg.Server.HTTP
		slog.Info("directive proxy service starting", "listen", srv.Addr, "https", cfg.TLS.Enabled, "proxy_prefix", app.cfg.Proxy.PathPrefix)
		var serveErr error
		if cfg.TLS.Enabled {
			serveErr = srv.ServeTLS(ln, "", "")
		} else {
			serveErr = srv.Serve(ln)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		return shutdown(ctx, srv, rt)
	case sig := <-sigCh:
		slog.Info("received shutdown signal", "signal", sig.String())
		return shutdown(ctx, srv, rt)
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

func shutdown(ctx context.Context, srv *http.Server, rt *runtime) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var errs []error
	if err := srv.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, err)
	}
	if err := rt.Close(shutdownCtx); err != nil {
		errs = append(errs, err)
	}
	slog.Info("directive proxy service stopped")
	return errors.Join(errs...)
}
