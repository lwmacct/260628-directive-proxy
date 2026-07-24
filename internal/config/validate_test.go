package config

import (
	"errors"
	"testing"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260718-go-pkg-clientip/pkg/clientip"
	"github.com/lwmacct/260718-go-pkg-ipallow/pkg/ipallow"
)

func validDefaultConfig() Server {
	return DefaultConfig().Server
}

func TestDefaultConfigUsesSingleHTTPListen(t *testing.T) {
	cfg := DefaultConfig().Server

	if cfg.HTTP.Listen != ":23198" {
		t.Fatalf("unexpected http listen: %q", cfg.HTTP.Listen)
	}
	if cfg.Metrics.Prefix != "m_260628_" {
		t.Fatalf("unexpected metrics prefix: %q", cfg.Metrics.Prefix)
	}
}

func TestValidateNormalizesMetricsPrefix(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.Metrics.Prefix = " edge_proxy_ "

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Metrics.Prefix != "edge_proxy_" {
		t.Fatalf("unexpected metrics prefix: %q", validated.Metrics.Prefix)
	}
}

func TestValidateRejectsInvalidMetricsPrefix(t *testing.T) {
	for _, prefix := range []string{"", "9proxy", "edge-proxy", "edge.proxy", "edge proxy", "代理"} {
		cfg := validDefaultConfig()
		cfg.Metrics.Prefix = prefix
		if _, err := Validate(cfg); !errors.Is(err, ErrInvalidMetrics) {
			t.Fatalf("expected invalid metrics prefix %q, got %v", prefix, err)
		}
	}
}

func TestValidateRejectsMissingHTTPListen(t *testing.T) {
	cfg := validDefaultConfig()
	cfg.HTTP.Listen = " "

	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid http config, got %v", err)
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

func TestValidateRejectsInvalidSourceAccess(t *testing.T) {
	tests := []func(*DirectiveSourceAccess){
		func(cfg *DirectiveSourceAccess) { cfg.Rules = nil },
		func(cfg *DirectiveSourceAccess) { cfg.Rules = []ipallow.Rule{{Value: "bad_name.example"}} },
		func(cfg *DirectiveSourceAccess) { cfg.TrustedProxies = []string{"proxy.example.com"} },
		func(cfg *DirectiveSourceAccess) { cfg.Headers = []clientip.Header{"invalid"} },
		func(cfg *DirectiveSourceAccess) { cfg.MaxHops = -1 },
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
	cfg.Proxy.Directive.SourceAccess.Rules = []ipallow.Rule{{Value: " EDGE.Example.COM. "}, {Value: "192.0.2.7/24"}}
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
		func(cfg *RemoteDirective) { cfg.MaxPayloadBytes = 0 },
		func(cfg *RemoteDirective) { cfg.Redis.ClientCacheCapacity = 0 },
		func(cfg *RemoteDirective) { cfg.Redis.ClientIdleTimeout = -1 },
		func(cfg *RemoteDirective) { cfg.Redis.PoolSize = 0 },
		func(cfg *RemoteDirective) { cfg.File.Root = "" },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Directive.Remote)
		if _, err := Validate(cfg); err != ErrInvalidDirective {
			t.Fatalf("expected invalid directive config, got %v", err)
		}
	}
	cfg := validDefaultConfig()
	cfg.Proxy.Directive.Remote.File.Root = "  /srv/directives  "
	validated, err := Validate(cfg)
	if err != nil || validated.Proxy.Directive.Remote.File.Root != "/srv/directives" {
		t.Fatalf("file root was not normalized: root=%q err=%v", validated.Proxy.Directive.Remote.File.Root, err)
	}
	for _, mutate := range []func(*ProxyDirective){
		func(cfg *ProxyDirective) { cfg.TokenSecret = " " },
		func(cfg *ProxyDirective) { cfg.MaxTokenBytes = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Directive)
		if _, err := Validate(cfg); err != ErrInvalidDirective {
			t.Fatalf("expected invalid directive config, got %v", err)
		}
	}
}

func TestValidateRejectsInvalidProxyTransport(t *testing.T) {
	for _, mutate := range []func(*ProxyTransport){
		func(cfg *ProxyTransport) { cfg.MaxIdleConns = 0 },
		func(cfg *ProxyTransport) { cfg.MaxIdleConnsPerHost = 0 },
		func(cfg *ProxyTransport) { cfg.MaxIdleConnsPerHost = cfg.MaxIdleConns + 1 },
		func(cfg *ProxyTransport) { cfg.MaxConnsPerHost = -1 },
		func(cfg *ProxyTransport) { cfg.IdleConnTimeout = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Transport)
		if _, err := Validate(cfg); err != ErrInvalidTransport {
			t.Fatalf("expected invalid proxy transport config, got %v", err)
		}
	}
}

func TestValidateRecoveryConfiguration(t *testing.T) {
	for _, mutate := range []func(*ProxyRecovery){
		func(cfg *ProxyRecovery) { cfg.MaxRoundTripsLimit = 0 },
		func(cfg *ProxyRecovery) { cfg.MaxElapsedLimit = 0 },
		func(cfg *ProxyRecovery) { cfg.MaxCallbackTimeout = 0 },
		func(cfg *ProxyRecovery) { cfg.MaxCapturedBodyBytes = 0 },
		func(cfg *ProxyRecovery) { cfg.MaxCallbackResponseBytes = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.Recovery)
		if _, err := Validate(cfg); err != ErrInvalidRecovery {
			t.Fatalf("expected invalid recovery config, got %v", err)
		}
	}
	for _, mutate := range []func(*ProxyBodyStore){
		func(cfg *ProxyBodyStore) { cfg.MemoryMaxBytes = 0 },
		func(cfg *ProxyBodyStore) { cfg.QueueMaxRequests = 0 },
		func(cfg *ProxyBodyStore) { cfg.QueueWait = 0 },
		func(cfg *ProxyBodyStore) { cfg.MaxBodyBytes = cfg.MemoryMaxBytes + 1 },
		func(cfg *ProxyBodyStore) { cfg.MaxBodyBytes = 0 },
		func(cfg *ProxyBodyStore) { cfg.ChunkBytes = 0 },
		func(cfg *ProxyBodyStore) { cfg.ChunkBytes = 2 << 20 },
		func(cfg *ProxyBodyStore) { cfg.ReadTimeout = 0 },
	} {
		cfg := validDefaultConfig()
		mutate(&cfg.Proxy.BodyStore)
		if _, err := Validate(cfg); err != ErrInvalidBodyStore {
			t.Fatalf("expected invalid body store config, got %v", err)
		}
	}
}
