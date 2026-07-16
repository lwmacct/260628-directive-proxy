package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
)

var files = cfgm.ConfigFiles[Config]{
	Manager:     Manager,
	ExampleFile: "config/config.example.yaml",
	RuntimeFile: "config/config.yaml",
}

func TestWriteConfigExample(t *testing.T)     { files.WriteExample(t) }
func TestRuntimeConfigKeysValid(t *testing.T) { files.ValidateRuntimeConfig(t) }

func TestConfigFileUsesCommandHierarchy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  proxy:\n    retry:\n      max-attempts: 7\n  fluent:\n    ack: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Proxy.Retry.MaxAttempts != 7 {
		t.Fatalf("unexpected retry max attempts: %d", loaded.Server.Proxy.Retry.MaxAttempts)
	}
	if !loaded.Server.Fluent.ACK {
		t.Fatal("Fluent config was not loaded from the server hierarchy")
	}

	if err := os.WriteFile(path, []byte("proxy:\n  retry:\n    max-attempts: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Manager.Load(t.Context(), cfgm.File(path))
	if err == nil || !strings.Contains(err.Error(), "proxy") {
		t.Fatalf("legacy root config must be rejected, got %v", err)
	}
}

func TestConfigFileLoadsInlineTLSConfiguration(t *testing.T) {
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

func TestConfigFileRejectsRemovedFluentOutputConfiguration(t *testing.T) {
	for _, content := range []string{
		"server:\n  fluent:\n    connections: 4\n",
		"server:\n  fluent:\n    queue:\n      max-records: 8192\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Manager.Load(t.Context(), cfgm.File(path)); err == nil {
			t.Fatalf("removed Fluent output config must be rejected: %s", content)
		}
	}
}

func TestConfigFileRejectsLegacyFlatFluentClientConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  fluent:\n    retry-max-attempts: 3\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Manager.Load(t.Context(), cfgm.File(path))
	if err == nil || !strings.Contains(err.Error(), "server.fluent.retry-max-attempts") {
		t.Fatalf("legacy flat Fluent config must be rejected, got %v", err)
	}
}
