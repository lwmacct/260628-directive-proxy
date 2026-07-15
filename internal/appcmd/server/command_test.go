package server

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

func TestServerManagerPreservesCLIPaths(t *testing.T) {
	_, server := configuredServer(t, func(context.Context, *cli.Command, *config.Config) error { return nil })
	flags := server.Flags
	for _, name := range []string{
		"http.auth.methods",
		"proxy.retry.max-attempts",
		"fluent.connections",
	} {
		if findFlag(flags, name) == nil {
			t.Fatalf("missing CLI flag --%s", name)
		}
	}
	if findFlag(flags, "server.http.auth.methods") != nil {
		t.Fatal("server scope must not leak into CLI flag names")
	}
	for _, name := range []string{"http.auth.session.keys", "http.auth.token"} {
		if findFlag(flags, name) != nil {
			t.Fatalf("sensitive config must not expose CLI flag --%s", name)
		}
	}
	for _, name := range []string{"observability.fluent.enabled", "observability.instance-id", "instance-id"} {
		if findFlag(flags, name) != nil {
			t.Fatalf("removed config must not expose CLI flag --%s", name)
		}
	}
}

func TestServerManagerLoadsCommandConfig(t *testing.T) {
	var loaded *config.Config
	root, _ := configuredServer(t, func(_ context.Context, _ *cli.Command, cfg *config.Config) error {
		loaded = cfg
		return nil
	})

	err := root.Run(t.Context(), []string{
		"app",
		"--env-prefix=",
		"server",
		"--http.auth.methods=oidc",
		"--proxy.retry.max-attempts=4",
		"--fluent.connections=6",
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("manager did not load configuration")
	}
	if len(loaded.Server.HTTP.Auth.Methods) != 1 || loaded.Server.HTTP.Auth.Methods[0] != config.AuthMethodOIDC {
		t.Fatalf("unexpected authentication methods: %#v", loaded.Server.HTTP.Auth.Methods)
	}
	if loaded.Server.Proxy.Retry.MaxAttempts != 4 {
		t.Fatalf("unexpected retry max attempts: %d", loaded.Server.Proxy.Retry.MaxAttempts)
	}
	if loaded.Server.Fluent.Connections != 6 {
		t.Fatalf("unexpected Fluent connections: %d", loaded.Server.Fluent.Connections)
	}
}

func TestServerManagerUsesFullCommandPathForEnvironment(t *testing.T) {
	t.Setenv("TEST_SERVER_PROXY_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("TEST_SERVER_FLUENT_CONNECTIONS", "6")
	var loaded *config.Config
	root, _ := configuredServer(t, func(_ context.Context, _ *cli.Command, cfg *config.Config) error {
		loaded = cfg
		return nil
	})

	if err := root.Run(t.Context(), []string{"app", "--env-prefix=TEST_", "server"}); err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Server.Proxy.Retry.MaxAttempts != 5 || loaded.Server.Fluent.Connections != 6 {
		t.Fatalf("unexpected environment config: %#v", loaded)
	}
}

func TestServerManagerRejectsLegacyScopedFlag(t *testing.T) {
	root, _ := configuredServer(t, func(context.Context, *cli.Command, *config.Config) error { return nil })
	err := root.Run(t.Context(), []string{"app", "server", "--server.http.auth.methods=oidc"})
	if err == nil || !strings.Contains(err.Error(), "server.http.auth.methods") {
		t.Fatalf("legacy scoped flag must be rejected, got %v", err)
	}
}

func TestServerManagerRejectsRemovedObservabilityFlag(t *testing.T) {
	root, _ := configuredServer(t, func(context.Context, *cli.Command, *config.Config) error { return nil })
	err := root.Run(t.Context(), []string{"app", "server", "--observability.fluent.enabled=true"})
	if err == nil || !strings.Contains(err.Error(), "observability.fluent.enabled") {
		t.Fatalf("removed observability flag must be rejected, got %v", err)
	}
}

func configuredServer(
	t *testing.T,
	action func(context.Context, *cli.Command, *config.Config) error,
) (*cli.Command, *cli.Command) {
	t.Helper()
	server := &cli.Command{Name: "server", Action: config.Manager.Action(action)}
	root := &cli.Command{Name: "app", Commands: []*cli.Command{server}}
	config.Manager.MustConfigure(root)
	return root, server
}

func findFlag(flags []cli.Flag, name string) cli.Flag {
	for _, flag := range flags {
		if flag.Names()[0] == name {
			return flag
		}
	}
	return nil
}
