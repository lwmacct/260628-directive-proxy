package config

import (
	"strings"
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

func TestValidateTokenAuth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.AuthMode = AuthModeToken
	cfg.Server.HTTP.TokenAuth.Tokens = []string{"  " + strings.Repeat("a", 32) + "  "}
	cfg.Server.HTTP.OIDCAuth = dexgithub.Config{}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if validated.Server.HTTP.TokenAuth.Tokens[0] != strings.Repeat("a", 32) {
		t.Fatalf("unexpected normalized token")
	}
}

func TestValidateRejectsInvalidAuthModeAndActiveTokenConfig(t *testing.T) {
	for _, mutate := range []func(*ServerHTTP){
		func(cfg *ServerHTTP) { cfg.AuthMode = "unknown" },
		func(cfg *ServerHTTP) {
			cfg.AuthMode = AuthModeToken
			cfg.TokenAuth.Tokens = nil
		},
	} {
		cfg := DefaultConfig()
		mutate(&cfg.Server.HTTP)
		if _, err := Validate(cfg); err != ErrInvalidAuth {
			t.Fatalf("expected invalid auth config, got %v", err)
		}
	}
}

func TestValidateRejectsInvalidSourceAccess(t *testing.T) {
	tests := []func(*DirectiveSourceAccess){
		func(cfg *DirectiveSourceAccess) { cfg.AllowedSources = nil },
		func(cfg *DirectiveSourceAccess) { cfg.AllowedSources = []string{"bad_name.example"} },
		func(cfg *DirectiveSourceAccess) { cfg.TrustedProxies = []string{"proxy.example.com"} },
		func(cfg *DirectiveSourceAccess) { cfg.DNS.LookupTimeout = -1 },
		func(cfg *DirectiveSourceAccess) { cfg.DNS.MaxHosts = 0 },
	}
	for _, mutate := range tests {
		cfg := DefaultConfig()
		cfg.Proxy.Directive.SourceAccess.Enabled = true
		mutate(&cfg.Proxy.Directive.SourceAccess)
		if _, err := Validate(cfg); err != ErrInvalidAccess {
			t.Fatalf("expected invalid source access config, got %v", err)
		}
	}
}

func TestValidateNormalizesSourceAccess(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	cfg.Proxy.Directive.SourceAccess.AllowedSources = []string{" EDGE.Example.COM. ", "192.0.2.7/24"}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"10.0.0.1"}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	access := validated.Proxy.Directive.SourceAccess
	if access.AllowedSources[0] != "edge.example.com" || access.AllowedSources[1] != "192.0.2.0/24" ||
		access.TrustedProxies[0] != "10.0.0.1/32" {
		t.Fatalf("unexpected normalized source access: %#v", access)
	}
}

func TestValidateSkipsSourceAccessWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.Directive.SourceAccess.AllowedSources = nil
	cfg.Proxy.Directive.SourceAccess.DNS.MaxHosts = 0

	if _, err := Validate(cfg); err != nil {
		t.Fatalf("disabled source access must not be validated: %v", err)
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
