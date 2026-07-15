package config

import (
	"testing"

	"github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"
)

var testDefinition = cfgm.New(DefaultConfig(), cfgm.WithoutDefaultPaths())

var files = cfgm.ConfigFiles[Config]{
	Definition:  testDefinition,
	ExampleFile: "config/config.example.yaml",
	RuntimeFile: "config/config.yaml",
}

func TestWriteConfigExample(t *testing.T)     { files.WriteExample(t) }
func TestRuntimeConfigKeysValid(t *testing.T) { files.ValidateRuntimeConfig(t) }
