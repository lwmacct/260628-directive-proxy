package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/templexp"
)

var files = cfgm.ConfigFiles[Config]{
	Manager:     Manager,
	ExampleFile: "config/config.example.yaml",
	RuntimeFile: "config/config.yaml",
}

func TestWriteConfigExample(t *testing.T)     { files.WriteExample(t) }
func TestRuntimeConfigKeysValid(t *testing.T) { files.ValidateRuntimeConfig(t) }

func TestDefaultFluentTagPrefixUsesDP(t *testing.T) {
	if prefix := DefaultConfig().Server.Fluent.TagPrefix; prefix != "dp" {
		t.Fatalf("unexpected Fluent tag prefix: %q", prefix)
	}
}

func setDirectiveTokenSecret(t *testing.T) {
	t.Helper()
	t.Setenv("DIRECTIVE_TOKEN_SECRET", "test-directive-token-secret")
}

func TestConfigReportsMissingDirectiveTokenSecret(t *testing.T) {
	t.Setenv("DIRECTIVE_TOKEN_SECRET", "")

	_, err := Manager.Load(t.Context())
	if err == nil {
		t.Fatal("expected missing directive token secret error")
	}
	var requiredErr *templexp.RequiredError
	if !errors.As(err, &requiredErr) {
		t.Fatalf("expected required template error, got %v", err)
	}
	if requiredErr.Name != "DIRECTIVE_TOKEN_SECRET" {
		t.Fatalf("unexpected required variable: %q", requiredErr.Name)
	}
	if !strings.Contains(err.Error(), "root.server.proxy.directive.token-secret") {
		t.Fatalf("expected directive token-secret config path, got %v", err)
	}
}

func TestConfigFileOverridesDirectiveTokenSecretTemplate(t *testing.T) {
	t.Setenv("DIRECTIVE_TOKEN_SECRET", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  proxy:\n    directive:\n      token-secret: from-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatalf("file token secret must override the default template: %v", err)
	}
	if loaded.Server.Proxy.Directive.TokenSecret != "from-file" {
		t.Fatalf("unexpected directive token secret: %q", loaded.Server.Proxy.Directive.TokenSecret)
	}
}

func TestConfigFileUsesCommandHierarchy(t *testing.T) {
	setDirectiveTokenSecret(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  proxy:\n    recovery:\n      max-round-trips-limit: 7\n  fluent:\n    ack: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Proxy.Recovery.MaxRoundTripsLimit != 7 {
		t.Fatalf("unexpected recovery max round trips: %d", loaded.Server.Proxy.Recovery.MaxRoundTripsLimit)
	}
	if !loaded.Server.Fluent.ACK {
		t.Fatal("Fluent config was not loaded from the server hierarchy")
	}
}

func TestConfigFileLoadsInlineTLSConfiguration(t *testing.T) {
	setDirectiveTokenSecret(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  http:\n    tls:\n      enabled: true\n      poll-interval: 15s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Server.HTTP.TLS.Enabled || loaded.Server.HTTP.TLS.PollInterval != 15*time.Second {
		t.Fatalf("unexpected TLS config: %#v", loaded.Server.HTTP.TLS)
	}
	if loaded.Server.HTTP.TLS.DefaultCertificate != "default" || len(loaded.Server.HTTP.TLS.Certificates) != 1 {
		t.Fatalf("TLS defaults were not preserved: %#v", loaded.Server.HTTP.TLS)
	}
}

func TestConfigFileLoadsInlineFluentClientConfiguration(t *testing.T) {
	setDirectiveTokenSecret(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  fluent:\n    ack: true\n    retry:\n      max-attempts: 3\n    timeout:\n      connect: 2s\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	fluentConfig := loaded.Server.Fluent
	if !fluentConfig.ACK || fluentConfig.Retry.MaxAttempts != 3 || fluentConfig.Timeout.Connect != 2*time.Second {
		t.Fatalf("unexpected Fluent client config: %#v", fluentConfig)
	}
	if fluentConfig.Buffer.MaxEvents != 8192 || fluentConfig.Retry.MinBackoff != 100*time.Millisecond {
		t.Fatalf("Fluent defaults were not preserved: %#v", fluentConfig)
	}
}

func TestConfigFileLoadsProxyTransport(t *testing.T) {
	setDirectiveTokenSecret(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  proxy:\n    transport:\n      max-idle-conns: 321\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Proxy.Transport.MaxIdleConns != 321 {
		t.Fatalf("unexpected proxy transport: %#v", loaded.Server.Proxy.Transport)
	}
}
