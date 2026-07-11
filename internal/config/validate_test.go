package config

import (
	"testing"

	"github.com/lwmacct/260711-go-pkg-oidcauth/pkg/oidcauth/dexgithub"
)

func TestDefaultConfigUsesSingleHTTPListen(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.HTTP.Listen != ":23198" {
		t.Fatalf("unexpected http listen: %q", cfg.Server.HTTP.Listen)
	}
}

func TestValidateRejectsMissingHTTPListen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.Listen = " "

	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid http config, got %v", err)
	}
}

func TestValidateRejectsInvalidAuth(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dexgithub.Config)
	}{
		{name: "http issuer", mutate: func(cfg *dexgithub.Config) { cfg.Issuer = "http://auth.example.com" }},
		{name: "missing client", mutate: func(cfg *dexgithub.Config) { cfg.ClientID = "" }},
		{name: "remote http callback", mutate: func(cfg *dexgithub.Config) { cfg.CallbackURL = "http://tool.example.com/auth/callback" }},
		{name: "public URL path", mutate: func(cfg *dexgithub.Config) { cfg.PublicURL = "https://tool.example.com/app" }},
		{name: "callback host mismatch", mutate: func(cfg *dexgithub.Config) { cfg.PublicURL = "http://127.0.0.1:23199" }},
		{name: "missing users", mutate: func(cfg *dexgithub.Config) { cfg.AllowedUsers = nil }},
		{name: "empty user", mutate: func(cfg *dexgithub.Config) { cfg.AllowedUsers = []string{" "} }},
		{name: "duplicate users", mutate: func(cfg *dexgithub.Config) { cfg.AllowedUsers = []string{"lwmacct", " LwMacct "} }},
		{name: "invalid session TTL", mutate: func(cfg *dexgithub.Config) { cfg.SessionTTL = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			test.mutate(&cfg.Server.HTTP.Auth)
			if _, err := Validate(cfg); err != ErrInvalidAuth {
				t.Fatalf("expected invalid auth config, got %v", err)
			}
		})
	}
}

func TestValidateNormalizesAuth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.Auth.Issuer += "/"
	cfg.Server.HTTP.Auth.AllowedUsers = []string{" LwMacct "}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if validated.Server.HTTP.Auth.Issuer != "https://2008.s.lwmacct.com:20088" {
		t.Fatalf("unexpected issuer: %q", validated.Server.HTTP.Auth.Issuer)
	}
	if validated.Server.HTTP.Auth.AllowedUsers[0] != "lwmacct" {
		t.Fatalf("unexpected username: %q", validated.Server.HTTP.Auth.AllowedUsers[0])
	}
}
