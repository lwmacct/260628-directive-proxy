package config

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/dexgithub"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/statictoken"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
)

func validDefaultConfig() Server {
	cfg := DefaultConfig().Server
	cfg.HTTP.AuthMe.Session.Keys[0].Secret = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.HTTP.AuthMe.StaticToken.Credentials = []statictoken.Credential{{ID: "admin", Name: "Administrator", Token: "admin-token"}}
	return cfg
}

func oidcConfig() Server {
	cfg := validDefaultConfig()
	cfg.HTTP.AuthMe.StaticToken.Enabled = false
	cfg.HTTP.AuthMe.DexGitHub.Enabled = true
	cfg.HTTP.AuthMe.DexGitHub = testOIDCAuth()
	cfg.HTTP.AuthMe.AllowedGitHubUsers = []string{"lwmacct"}
	return cfg
}

func testOIDCAuth() dexgithub.Config {
	return dexgithub.Config{
		Enabled:    true,
		Issuer:     "https://2008.s.lwmacct.com:20088",
		ClientID:   "dproxy",
		SessionTTL: 24 * time.Hour,
	}
}

func TestDefaultConfigUsesSingleHTTPListen(t *testing.T) {
	cfg := DefaultConfig().Server

	if cfg.HTTP.Listen != ":23198" {
		t.Fatalf("unexpected http listen: %q", cfg.HTTP.Listen)
	}
}

func TestValidateRejectsMissingHTTPListen(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.Listen = " "

	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid http config, got %v", err)
	}
}

func TestValidateRejectsInvalidHTTPHeaderLimit(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.MaxHeaderBytes = 0
	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid HTTP config, got %v", err)
	}
}

func TestValidateNormalizesTLSConfiguration(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.TLS.Enabled = true
	cfg.HTTP.TLS.DefaultCertificate = " default "
	cfg.HTTP.TLS.Certificates = []tlsreload.CertificateSource{
		{ID: " default ", Certificate: " cert.pem ", PrivateKey: " key.pem "},
	}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	tlsConfig := validated.HTTP.TLS
	if tlsConfig.DefaultCertificate != "default" || tlsConfig.Certificates[0].ID != "default" ||
		tlsConfig.Certificates[0].Certificate != "cert.pem" || tlsConfig.Certificates[0].PrivateKey != "key.pem" {
		t.Fatalf("unexpected normalized TLS config: %#v", tlsConfig)
	}
}

func TestValidateSkipsTLSConfigurationWhenDisabled(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.TLS.Certificates = nil
	cfg.HTTP.TLS.DefaultCertificate = ""

	if _, err := Validate(cfg); err != nil {
		t.Fatalf("disabled TLS must not be validated: %v", err)
	}
}

func TestValidateRejectsInvalidAuth(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dexgithub.Config)
	}{
		{name: "http issuer", mutate: func(cfg *dexgithub.Config) { cfg.Issuer = "http://auth.example.com" }},
		{name: "missing client", mutate: func(cfg *dexgithub.Config) { cfg.ClientID = "" }},
		{name: "missing users", mutate: func(*dexgithub.Config) {}},
		{name: "empty user", mutate: func(*dexgithub.Config) {}},
		{name: "duplicate users", mutate: func(*dexgithub.Config) {}},
		{name: "invalid session TTL", mutate: func(cfg *dexgithub.Config) { cfg.SessionTTL = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := oidcConfig()
			test.mutate(&cfg.HTTP.AuthMe.DexGitHub)
			switch test.name {
			case "missing users":
				cfg.HTTP.AuthMe.AllowedGitHubUsers = nil
			case "empty user":
				cfg.HTTP.AuthMe.AllowedGitHubUsers = []string{" "}
			case "duplicate users":
				cfg.HTTP.AuthMe.AllowedGitHubUsers = []string{"lwmacct", " LwMacct "}
			}
			if _, err := Validate(cfg); !errors.Is(err, ErrInvalidAuth) {
				t.Fatalf("expected invalid auth config, got %v", err)
			}
		})
	}
}

func TestValidateNormalizesAuth(t *testing.T) {
	cfg := oidcConfig()
	cfg.HTTP.AuthMe.DexGitHub.Issuer += "/"
	cfg.HTTP.AuthMe.AllowedGitHubUsers = []string{" LwMacct "}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if validated.HTTP.AuthMe.DexGitHub.Issuer != "https://2008.s.lwmacct.com:20088" {
		t.Fatalf("unexpected issuer: %q", validated.HTTP.AuthMe.DexGitHub.Issuer)
	}
	if validated.HTTP.AuthMe.AllowedGitHubUsers[0] != "lwmacct" {
		t.Fatalf("unexpected username: %q", validated.HTTP.AuthMe.AllowedGitHubUsers[0])
	}
}

func TestValidateTokenAuth(t *testing.T) {
	cfg := validDefaultConfig()

	if _, err := Validate(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	cfg.HTTP.AuthMe.StaticToken.Credentials[0].Token = "admin-token-rotated"
	if _, err := Validate(cfg); err != nil {
		t.Fatalf("opaque token was rejected: %v", err)
	}
	cfg.HTTP.AuthMe.StaticToken.Credentials[0].Token = "invalid token"
	if _, err := Validate(cfg); !errors.Is(err, ErrInvalidAuth) {
		t.Fatalf("token containing whitespace was accepted: %v", err)
	}
}

func TestValidateOIDCAndTokenAuth(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.AuthMe.DexGitHub.Enabled = true
	cfg.HTTP.AuthMe.DexGitHub = testOIDCAuth()
	cfg.HTTP.AuthMe.AllowedGitHubUsers = []string{"lwmacct"}

	if _, err := Validate(cfg); err != nil {
		t.Fatalf("validate combined auth config: %v", err)
	}
}

func TestValidateRejectsDisabledAuth(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.AuthMe.StaticToken.Enabled = false
	if _, err := Validate(cfg); !errors.Is(err, ErrInvalidAuth) {
		t.Fatalf("expected invalid auth config, got %v", err)
	}
}

func TestValidateReportsAuthCause(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.AuthMe.Origins = []string{"http://localhost:23199", "https://2310.s.lwmacct.com:23109"}

	_, err := Validate(cfg)
	if !errors.Is(err, ErrInvalidAuth) {
		t.Fatalf("expected invalid auth config, got %v", err)
	}
	want := `invalid auth config: authme: invalid authme config: trusted origins must use one scheme: origin[1]="https://2310.s.lwmacct.com:23109" conflicts with origin[0]="http://localhost:23199"`
	if err.Error() != want {
		t.Fatalf("unexpected diagnostic:\nwant: %s\n got: %s", want, err)
	}
}

func TestValidateRejectsInvalidSourceAccess(t *testing.T) {
	tests := []func(*DirectiveSourceAccess){
		func(cfg *DirectiveSourceAccess) { cfg.Rules = nil },
		func(cfg *DirectiveSourceAccess) { cfg.Rules = []sourceaccess.Rule{{Value: "bad_name.example"}} },
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
	cfg.Proxy.Directive.SourceAccess.Rules = []sourceaccess.Rule{{Value: " EDGE.Example.COM. "}, {Value: "192.0.2.7/24"}}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"10.0.0.1"}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	access := validated.Proxy.Directive.SourceAccess
	if access.Rules[0].Value != "edge.example.com" || access.Rules[1].Value != "192.0.2.0/24" ||
		access.TrustedProxies[0] != "10.0.0.1/32" {
		t.Fatalf("unexpected normalized source access: %#v", access)
	}
}

func TestValidateSkipsSourceAccessWhenDisabled(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.Proxy.Directive.SourceAccess.Rules = nil
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
		func(cfg *ProxyRetry) { cfg.MaxAttempts = 1 },
		func(cfg *ProxyRetry) { cfg.CommandRetention = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Retry)
		if _, err := Validate(cfg); err != ErrInvalidRetry {
			t.Fatalf("expected invalid retry config, got %v", err)
		}
	}
	for _, mutate := range []func(*ProxyBodyMemory){
		func(cfg *ProxyBodyMemory) { cfg.MaxActiveBytes = 0 },
		func(cfg *ProxyBodyMemory) { cfg.MaxBodyBytes = 0 },
		func(cfg *ProxyBodyMemory) { cfg.MaxBodyBytes = cfg.MaxActiveBytes + 1 },
		func(cfg *ProxyBodyMemory) { cfg.QueueMax = 0 },
		func(cfg *ProxyBodyMemory) { cfg.QueueWait = 0 },
		func(cfg *ProxyBodyMemory) { cfg.ReadTimeout = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.BodyMemory)
		if _, err := Validate(cfg); err != ErrInvalidRetry {
			t.Fatalf("expected invalid body memory config, got %v", err)
		}
	}
}
