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

func TestValidateRejectsInvalidHTTPHeaderLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.MaxHeaderBytes = 0
	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid HTTP config, got %v", err)
	}
}

func TestValidateRejectsInvalidAuth(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dexgithub.Config)
	}{
		{name: "http issuer", mutate: func(cfg *dexgithub.Config) { cfg.Issuer = "http://auth.example.com" }},
		{name: "missing client", mutate: func(cfg *dexgithub.Config) { cfg.ClientID = "" }},
		{name: "remote http external URL", mutate: func(cfg *dexgithub.Config) { cfg.ExternalURLs = []string{"http://tool.example.com"} }},
		{name: "external URL path", mutate: func(cfg *dexgithub.Config) { cfg.ExternalURLs = []string{"https://tool.example.com/app"} }},
		{name: "duplicate external URL host", mutate: func(cfg *dexgithub.Config) {
			cfg.ExternalURLs = []string{"https://tool.example.com", "https://tool.example.com/"}
		}},
		{name: "missing users", mutate: func(cfg *dexgithub.Config) { cfg.AllowedUsers = nil }},
		{name: "empty user", mutate: func(cfg *dexgithub.Config) { cfg.AllowedUsers = []string{" "} }},
		{name: "duplicate users", mutate: func(cfg *dexgithub.Config) { cfg.AllowedUsers = []string{"lwmacct", " LwMacct "} }},
		{name: "invalid session TTL", mutate: func(cfg *dexgithub.Config) { cfg.SessionTTL = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			test.mutate(&cfg.Server.HTTP.OIDCAuth)
			if _, err := Validate(cfg); err != ErrInvalidAuth {
				t.Fatalf("expected invalid auth config, got %v", err)
			}
		})
	}
}

func TestValidateNormalizesAuth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.OIDCAuth.Issuer += "/"
	cfg.Server.HTTP.OIDCAuth.AllowedUsers = []string{" LwMacct "}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if validated.Server.HTTP.OIDCAuth.Issuer != "https://2008.s.lwmacct.com:20088" {
		t.Fatalf("unexpected issuer: %q", validated.Server.HTTP.OIDCAuth.Issuer)
	}
	if validated.Server.HTTP.OIDCAuth.AllowedUsers[0] != "lwmacct" {
		t.Fatalf("unexpected username: %q", validated.Server.HTTP.OIDCAuth.AllowedUsers[0])
	}
}

func TestValidateRemoteDirectiveResourceLimits(t *testing.T) {
	for _, mutate := range []func(*RemoteDirective){
		func(cfg *RemoteDirective) { cfg.Timeout = 0 },
		func(cfg *RemoteDirective) { cfg.HTTP.MaxRequestBytes = 0 },
		func(cfg *RemoteDirective) { cfg.MaxResponseBytes = 0 },
		func(cfg *RemoteDirective) { cfg.Redis.ClientCacheCapacity = 0 },
		func(cfg *RemoteDirective) { cfg.Redis.ClientIdleTimeout = -1 },
		func(cfg *RemoteDirective) { cfg.Redis.PoolSize = 0 },
	} {
		cfg := DefaultConfig()
		mutate(&cfg.Proxy.Directive.Remote)
		if _, err := Validate(cfg); err != ErrInvalidDirective {
			t.Fatalf("expected invalid directive config, got %v", err)
		}
	}
	for _, mutate := range []func(*ProxyDirective){
		func(cfg *ProxyDirective) { cfg.MaxTokenBytes = 0 },
		func(cfg *ProxyDirective) { cfg.MaxInlineBytes = 0 },
		func(cfg *ProxyDirective) { cfg.MaxInlineBytes = cfg.MaxTokenBytes + 1 },
	} {
		cfg := DefaultConfig()
		mutate(&cfg.Proxy.Directive)
		if _, err := Validate(cfg); err != ErrInvalidDirective {
			t.Fatalf("expected invalid directive config, got %v", err)
		}
	}
}
