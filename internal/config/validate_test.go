package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/statictoken"
)

func validDefaultConfig() Config {
	cfg := DefaultConfig()
	cfg.Server.HTTP.Auth.Session.Keys[0].Secret = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.Server.HTTP.Auth.Token.Credentials = map[string]statictoken.Credential{
		"admin": {Name: "Administrator", TokenSHA256: strings.Repeat("a", 64)},
	}
	return cfg
}

func oidcConfig() Config {
	cfg := validDefaultConfig()
	cfg.Server.HTTP.Auth.Methods = []AuthMethod{AuthMethodOIDC}
	cfg.Server.HTTP.Auth.OIDC = testOIDCAuth()
	return cfg
}

func testOIDCAuth() OIDCAuth {
	return OIDCAuth{
		Issuer:       "https://2008.s.lwmacct.com:20088",
		ClientID:     "dproxy",
		AllowedUsers: []string{"lwmacct"},
		SessionTTL:   24 * time.Hour,
	}
}

func TestDefaultConfigUsesSingleHTTPListen(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.HTTP.Listen != ":23198" {
		t.Fatalf("unexpected http listen: %q", cfg.Server.HTTP.Listen)
	}
}

func TestValidateRejectsMissingHTTPListen(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.Server.HTTP.Listen = " "

	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid http config, got %v", err)
	}
}

func TestValidateRejectsInvalidHTTPHeaderLimit(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.Server.HTTP.MaxHeaderBytes = 0
	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid HTTP config, got %v", err)
	}
}

func TestValidateRejectsInvalidAuth(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*OIDCAuth)
	}{
		{name: "http issuer", mutate: func(cfg *OIDCAuth) { cfg.Issuer = "http://auth.example.com" }},
		{name: "missing client", mutate: func(cfg *OIDCAuth) { cfg.ClientID = "" }},
		{name: "missing users", mutate: func(cfg *OIDCAuth) { cfg.AllowedUsers = nil }},
		{name: "empty user", mutate: func(cfg *OIDCAuth) { cfg.AllowedUsers = []string{" "} }},
		{name: "duplicate users", mutate: func(cfg *OIDCAuth) { cfg.AllowedUsers = []string{"lwmacct", " LwMacct "} }},
		{name: "invalid session TTL", mutate: func(cfg *OIDCAuth) { cfg.SessionTTL = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := oidcConfig()
			test.mutate(&cfg.Server.HTTP.Auth.OIDC)
			if _, err := Validate(cfg); err != ErrInvalidAuth {
				t.Fatalf("expected invalid auth config, got %v", err)
			}
		})
	}
}

func TestValidateNormalizesAuth(t *testing.T) {
	cfg := oidcConfig()
	cfg.Server.HTTP.Auth.OIDC.Issuer += "/"
	cfg.Server.HTTP.Auth.OIDC.AllowedUsers = []string{" LwMacct "}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if validated.Server.HTTP.Auth.OIDC.Issuer != "https://2008.s.lwmacct.com:20088" {
		t.Fatalf("unexpected issuer: %q", validated.Server.HTTP.Auth.OIDC.Issuer)
	}
	if validated.Server.HTTP.Auth.OIDC.AllowedUsers[0] != "lwmacct" {
		t.Fatalf("unexpected username: %q", validated.Server.HTTP.Auth.OIDC.AllowedUsers[0])
	}
}

func TestValidateTokenAuth(t *testing.T) {
	cfg := validDefaultConfig()

	if _, err := Validate(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	cfg.Server.HTTP.Auth.Token.Credentials["admin"] = statictoken.Credential{
		Name: "Administrator", TokenSHA256: strings.Repeat("A", 64),
	}
	if _, err := Validate(cfg); err != ErrInvalidAuth {
		t.Fatalf("uppercase token digest was accepted: %v", err)
	}
}

func TestValidateOIDCAndTokenAuth(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.Server.HTTP.Auth.Methods = []AuthMethod{AuthMethodToken, AuthMethodOIDC}
	cfg.Server.HTTP.Auth.OIDC = testOIDCAuth()

	if _, err := Validate(cfg); err != nil {
		t.Fatalf("validate combined auth config: %v", err)
	}
}

func TestValidateRejectsInvalidAuthMethods(t *testing.T) {
	for _, mutate := range []func(*Auth){
		func(cfg *Auth) { cfg.Methods = nil },
		func(cfg *Auth) { cfg.Methods = []AuthMethod{"unknown"} },
		func(cfg *Auth) { cfg.Methods = []AuthMethod{AuthMethodToken, AuthMethodToken} },
		func(cfg *Auth) {
			cfg.Methods = []AuthMethod{AuthMethodToken}
			cfg.Token.Credentials = nil
		},
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Server.HTTP.Auth)
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
		cfg := validDefaultConfig()
		cfg.Proxy.Directive.SourceAccess.Enabled = true
		mutate(&cfg.Proxy.Directive.SourceAccess)
		if _, err := Validate(cfg); err != ErrInvalidAccess {
			t.Fatalf("expected invalid source access config, got %v", err)
		}
	}
}

func TestValidateNormalizesSourceAccess(t *testing.T) {
	cfg := validDefaultConfig()
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
	cfg := validDefaultConfig()
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
		cfg := validDefaultConfig()
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
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Directive)
		if _, err := Validate(cfg); err != ErrInvalidDirective {
			t.Fatalf("expected invalid directive config, got %v", err)
		}
	}
}

func TestValidateRetryConfiguration(t *testing.T) {
	for _, mutate := range []func(*ProxyRetry){
		func(cfg *ProxyRetry) { cfg.RetryableAfter = -time.Second },
		func(cfg *ProxyRetry) { cfg.MaxAttempts = 1 },
		func(cfg *ProxyRetry) { cfg.MaxActiveRequests = 0 },
		func(cfg *ProxyRetry) { cfg.MaxBodyBytes = 0 },
		func(cfg *ProxyRetry) { cfg.MaxInflightBytes = cfg.MaxBodyBytes - 1 },
		func(cfg *ProxyRetry) { cfg.BufferChunkBytes = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Retry)
		if _, err := Validate(cfg); err != ErrInvalidRetry {
			t.Fatalf("expected invalid retry config, got %v", err)
		}
	}
	cfg := validDefaultConfig()
	cfg.Proxy.Retry.Enabled = false
	validated, err := Validate(cfg)
	if err != nil || validated.Proxy.Retry.MaxAttempts != 1 {
		t.Fatalf("disabled retry was not normalized: cfg=%#v err=%v", validated.Proxy.Retry, err)
	}
}

func TestValidateObservabilityConfiguration(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.Observability.Plugins[0].Enabled = true
	cfg.Observability.Plugins[1].Enabled = true
	cfg.Observability.Outputs[0].Enabled = true
	validated, err := Validate(cfg)
	if err != nil || validated.Observability.Outputs[0].Fluent == nil || validated.Observability.Outputs[0].Fluent.Endpoint != "unix:///run/fluent/fluent.sock" {
		t.Fatalf("valid observability config was rejected: cfg=%#v err=%v", validated.Observability, err)
	}
	for _, mutate := range []func(*Observability){
		func(cfg *Observability) { cfg.Plugins[0].Capture.BodyChunkBytes = 0 },
		func(cfg *Observability) { cfg.Plugins[0].Capture.MaxSSEEventBytes = 0 },
		func(cfg *Observability) { cfg.Plugins[0].Capture.RedactHeaders = []string{"[invalid"} },
		func(cfg *Observability) { cfg.Outputs[0].Fluent.Endpoint = "udp://127.0.0.1:24224" },
		func(cfg *Observability) { cfg.Outputs[0].Fluent.Connections = 0 },
		func(cfg *Observability) { cfg.Outputs[0].Fluent.ACKTimeout = 0 },
		func(cfg *Observability) { cfg.Outputs[0].Fluent.Delivery = "exactly-once" },
		func(cfg *Observability) { cfg.Outputs[0].Queue.MaxBytes = 0 },
	} {
		cfg := validDefaultConfig()
		cfg.Observability.Plugins[0].Enabled = true
		cfg.Observability.Outputs[0].Enabled = true
		mutate(&cfg.Observability)
		if _, err := Validate(cfg); err != ErrInvalidObservability {
			t.Fatalf("expected invalid observability config, got %v", err)
		}
	}
	unpaired := validDefaultConfig()
	unpaired.Observability.Plugins[0].Enabled = true
	if _, err := Validate(unpaired); err != ErrInvalidObservability {
		t.Fatalf("enabled plugin without output was accepted: %v", err)
	}
}
