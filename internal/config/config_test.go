package config

import (
	"context"
	"encoding/base64"
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

	if !slices.Equal(cfg.Server.HTTP.Auth.Methods, []AuthMethod{AuthMethodToken}) {
		t.Fatalf("unexpected default auth methods: %v", cfg.Server.HTTP.Auth.Methods)
	}
	if len(cfg.Server.HTTP.Auth.Token.Credentials) != 1 || cfg.Server.HTTP.Auth.Token.Credentials[0].Secret != "${API_ACCESS_TOKEN}" {
		t.Fatalf("unexpected default token auth config: %v", cfg.Server.HTTP.Auth.Token.Credentials)
	}
}

func TestDefaultAuthExpandsAPIAccessToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	t.Setenv("API_ACCESS_TOKEN", token)
	t.Setenv("AUTH_SESSION_KEY", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	validated, err := Validate(*cfg)
	if err != nil {
		t.Fatalf("validate loaded default config: %v", err)
	}
	if validated.Server.HTTP.Auth.Token.Credentials[0].Secret != token {
		t.Fatalf("unexpected expanded token: %v", validated.Server.HTTP.Auth.Token.Credentials)
	}
}

func TestDefaultAuthRequiresAPIAccessToken(t *testing.T) {
	t.Setenv("API_ACCESS_TOKEN", "")
	t.Setenv("AUTH_SESSION_KEY", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if _, err := Validate(*cfg); err != ErrInvalidAuth {
		t.Fatalf("expected missing API_ACCESS_TOKEN to fail auth validation, got %v", err)
	}
}

func TestDefaultAuthRequiresSessionKey(t *testing.T) {
	t.Setenv("API_ACCESS_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("AUTH_SESSION_KEY", "")

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if _, err := Validate(*cfg); err != ErrInvalidAuth {
		t.Fatalf("expected missing AUTH_SESSION_KEY to fail auth validation, got %v", err)
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
