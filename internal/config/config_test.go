package config

import (
	"context"
	"slices"
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
	if !slices.Equal(cfg.Server.HTTP.Auth.Token.Tokens, []string{"${API_ACCESS_TOKEN}"}) {
		t.Fatalf("unexpected default token auth config: %v", cfg.Server.HTTP.Auth.Token.Tokens)
	}
}

func TestDefaultAuthExpandsAPIAccessToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	t.Setenv("API_ACCESS_TOKEN", token)

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	validated, err := Validate(*cfg)
	if err != nil {
		t.Fatalf("validate loaded default config: %v", err)
	}
	if !slices.Equal(validated.Server.HTTP.Auth.Token.Tokens, []string{token}) {
		t.Fatalf("unexpected expanded tokens: %v", validated.Server.HTTP.Auth.Token.Tokens)
	}
}

func TestDefaultAuthRequiresAPIAccessToken(t *testing.T) {
	t.Setenv("API_ACCESS_TOKEN", "")

	cfg, err := cfgm.Load(context.Background(), DefaultConfig(), cfgm.NoDefaultPaths())
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if _, err := Validate(*cfg); err != ErrInvalidAuth {
		t.Fatalf("expected missing API_ACCESS_TOKEN to fail auth validation, got %v", err)
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
