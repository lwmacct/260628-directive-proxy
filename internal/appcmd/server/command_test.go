package server

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/urfave/cli/v3"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

func TestOIDCOnlyConfigurationFromCLI(t *testing.T) {
	t.Setenv("API_TOKEN_SHA256", "")
	t.Setenv("AUTH_SESSION_KEY", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	var loaded *config.Config
	server := &cli.Command{
		Name:  "server",
		Flags: commandFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := cfgm.Load(ctx, config.DefaultConfig(), cfgm.NoDefaultPaths(), cfgm.Command(cmd))
			if err != nil {
				return err
			}
			validated, err := config.Validate(*cfg)
			loaded = &validated
			return err
		},
	}
	root := &cli.Command{Name: "app", Commands: []*cli.Command{server}}
	err := root.Run(context.Background(), []string{
		"app", "server",
		"--http.auth.token.enabled=false",
		"--http.auth.oidc.enabled=true",
		"--http.auth.oidc.issuer=https://2008.s.lwmacct.com:20088",
		"--http.auth.oidc.client-id=dproxy",
		"--http.auth.oidc.allowed-users=lwmacct",
		"--http.auth.external-urls=https://2310.s.lwmacct.com:23109",
		"--http.auth.external-urls=https://2310.s.kuaicdn.cn:23109",
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Server.HTTP.Auth.Token.Enabled || !loaded.Server.HTTP.Auth.OIDC.Enabled {
		t.Fatalf("unexpected authentication config: %#v", loaded)
	}
	if loaded.Server.HTTP.Auth.OIDC.ClientID != "dproxy" ||
		!slices.Equal(loaded.Server.HTTP.Auth.ExternalURLs, []string{
			"https://2310.s.lwmacct.com:23109",
			"https://2310.s.kuaicdn.cn:23109",
		}) {
		t.Fatalf("unexpected OIDC CLI values: %#v", loaded.Server.HTTP.Auth)
	}
}
