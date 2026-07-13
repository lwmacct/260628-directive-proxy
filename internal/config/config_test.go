package config

import (
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
