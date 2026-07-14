package config

import (
	"context"
	"encoding/base64"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
)

var files = cfgm.ConfigFiles[Config]{
	Defaults:    DefaultConfig,
	ExampleFile: "config/config.example.yaml",
	RuntimeFile: "config/config.yaml",
}

func TestWriteConfigExample(t *testing.T)     { files.WriteExample(t) }
func TestRuntimeConfigKeysValid(t *testing.T) { files.ValidateRuntimeConfig(t) }

func TestDefaultAuthUsesAPIAccessToken(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Server.HTTP.Auth.Token.Enabled || cfg.Server.HTTP.Auth.OIDC.Enabled {
		t.Fatalf("unexpected default auth providers: %#v", cfg.Server.HTTP.Auth)
	}
	if len(cfg.Server.HTTP.Auth.Token.Credentials) != 1 || cfg.Server.HTTP.Auth.Token.Credentials["admin"].SecretSHA256 != "${API_TOKEN_SHA256}" {
		t.Fatalf("unexpected default token auth config: %v", cfg.Server.HTTP.Auth.Token.Credentials)
	}
}

func TestDefaultAuthExpandsAPIAccessToken(t *testing.T) {
	digest := strings.Repeat("a", 64)
	t.Setenv("API_TOKEN_SHA256", digest)
	t.Setenv("AUTH_SESSION_KEY", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	validated, err := Validate(*cfg)
	if err != nil {
		t.Fatalf("validate loaded default config: %v", err)
	}
	if validated.Server.HTTP.Auth.Token.Credentials["admin"].SecretSHA256 != digest {
		t.Fatalf("unexpected expanded token: %v", validated.Server.HTTP.Auth.Token.Credentials)
	}
}

func TestDefaultAuthRequiresAPIAccessToken(t *testing.T) {
	t.Setenv("API_TOKEN_SHA256", "")
	t.Setenv("AUTH_SESSION_KEY", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if _, err := Validate(*cfg); err != ErrInvalidAuth {
		t.Fatalf("expected missing API_TOKEN_SHA256 to fail auth validation, got %v", err)
	}
}

func TestDefaultAuthRequiresSessionKey(t *testing.T) {
	t.Setenv("API_TOKEN_SHA256", strings.Repeat("a", 64))
	t.Setenv("AUTH_SESSION_KEY", "")

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if _, err := Validate(*cfg); err != ErrInvalidAuth {
		t.Fatalf("expected missing AUTH_SESSION_KEY to fail auth validation, got %v", err)
	}
}

func TestConfigFileCanDisableTokenAndEnableOIDC(t *testing.T) {
	t.Setenv("AUTH_SESSION_KEY", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	path := t.TempDir() + "/config.yaml"
	content := []byte(`server:
  http:
    auth:
      token:
        enabled: false
      oidc:
        enabled: true
        issuer: https://auth.example.com
        client-id: dproxy
        allowed-users:
          - lwmacct
        session-ttl: 24h
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths(), cfgm.File(path, cfgm.Required()))
	if err != nil {
		t.Fatalf("load OIDC-only config: %v", err)
	}
	validated, err := Validate(*cfg)
	if err != nil {
		t.Fatalf("validate OIDC-only config: %v", err)
	}
	if validated.Server.HTTP.Auth.Token.Enabled || !validated.Server.HTTP.Auth.OIDC.Enabled {
		t.Fatalf("unexpected auth providers: %#v", validated.Server.HTTP.Auth)
	}
}

func TestDefaultSourceAccessIsDisabled(t *testing.T) {
	cfg := DefaultConfig()
	access := cfg.Proxy.Directive.SourceAccess

	if access.Enabled {
		t.Fatal("source access must be disabled by default")
	}
	if !slices.Contains(access.AllowedSources, "172.22.0.0/16") {
		t.Fatalf("default allowed sources do not contain 172.22.0.0/16: %v", access.AllowedSources)
	}
}
