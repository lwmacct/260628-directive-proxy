package server

import (
	"context"
	"testing"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

func TestServerBindingPreservesCLIPaths(t *testing.T) {
	flags := binding.Flags()
	for _, name := range []string{
		"http.auth.methods",
		"proxy.retry.max-attempts",
		"observability.instance-id",
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
}

func TestServerBindingLoadsCommandConfig(t *testing.T) {
	var loaded *config.Config
	server := &cli.Command{
		Name:  "server",
		Flags: binding.Flags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var err error
			loaded, err = binding.Load(ctx, cmd)
			return err
		},
	}
	root := &cli.Command{
		Name:     "app",
		Flags:    cfgm.RootFlags(),
		Commands: []*cli.Command{server},
	}

	err := root.Run(t.Context(), []string{
		"app",
		"--env-prefix=",
		"server",
		"--http.auth.methods=oidc",
		"--proxy.retry.max-attempts=4",
		"--observability.instance-id=test-instance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("binding did not load configuration")
	}
	if len(loaded.Server.HTTP.Auth.Methods) != 1 || loaded.Server.HTTP.Auth.Methods[0] != config.AuthMethodOIDC {
		t.Fatalf("unexpected authentication methods: %#v", loaded.Server.HTTP.Auth.Methods)
	}
	if loaded.Server.Proxy.Retry.MaxAttempts != 4 {
		t.Fatalf("unexpected retry max attempts: %d", loaded.Server.Proxy.Retry.MaxAttempts)
	}
	if loaded.Server.Observability.InstanceID != "test-instance" {
		t.Fatalf("unexpected observability instance ID: %q", loaded.Server.Observability.InstanceID)
	}
}

func TestServerBindingUsesFullCommandPathForEnvironment(t *testing.T) {
	t.Setenv("TEST_SERVER_PROXY_RETRY_MAX_ATTEMPTS", "5")
	var loaded *config.Config
	server := &cli.Command{
		Name:  "server",
		Flags: binding.Flags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var err error
			loaded, err = binding.Load(ctx, cmd)
			return err
		},
	}
	root := &cli.Command{
		Name:     "app",
		Flags:    cfgm.RootFlags(),
		Commands: []*cli.Command{server},
	}

	if err := root.Run(t.Context(), []string{"app", "--env-prefix=TEST_", "server"}); err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Server.Proxy.Retry.MaxAttempts != 5 {
		t.Fatalf("unexpected environment config: %#v", loaded)
	}
}

func findFlag(flags []cli.Flag, name string) cli.Flag {
	for _, flag := range flags {
		if flag.Names()[0] == name {
			return flag
		}
	}
	return nil
}
