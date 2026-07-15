package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if err := os.WriteFile(path, []byte("server:\n  proxy:\n    retry:\n      max-attempts: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Manager.Load(t.Context(), cfgm.File(path))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Proxy.Retry.MaxAttempts != 7 {
		t.Fatalf("unexpected retry max attempts: %d", loaded.Server.Proxy.Retry.MaxAttempts)
	}

	if err := os.WriteFile(path, []byte("proxy:\n  retry:\n    max-attempts: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Manager.Load(t.Context(), cfgm.File(path))
	if err == nil || !strings.Contains(err.Error(), "proxy") {
		t.Fatalf("legacy root config must be rejected, got %v", err)
	}
}
