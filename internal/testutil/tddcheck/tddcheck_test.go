package tddcheck_test

import (
	"testing"

	"github.com/lwmacct/260622-go-pkg-tddcheck/pkg/tddcheck"
)

func TestRules(t *testing.T) {
	tddcheck.Project{
		Root:   "internal",
		Config: projectConfig(),
	}.Assert(t)
}

func TestWriteProjectDoc(t *testing.T) {
	tddcheck.Project{
		Root:   "internal",
		Config: projectConfig(),
	}.WriteDoc(t, "")
}

func projectConfig() tddcheck.Config {
	config := tddcheck.DefaultConfig()
	config.DependencyLayerDirs = []string{"handler", "service", "repository", "core", "adapter", "appcmd"}
	config.LayerRules = append(config.LayerRules,
		tddcheck.LayerDependencyRule{SourceLayer: "handler", TargetLayer: "adapter", Message: "handler must not import adapter"},
		tddcheck.LayerDependencyRule{SourceLayer: "handler", TargetLayer: "appcmd", Message: "handler must not import appcmd"},
		tddcheck.LayerDependencyRule{SourceLayer: "service", TargetLayer: "adapter", Message: "service must not import adapter"},
		tddcheck.LayerDependencyRule{SourceLayer: "service", TargetLayer: "appcmd", Message: "service must not import appcmd"},
		tddcheck.LayerDependencyRule{SourceLayer: "adapter", TargetLayer: "handler", Message: "adapter must not import handler"},
		tddcheck.LayerDependencyRule{SourceLayer: "adapter", TargetLayer: "service", Message: "adapter must not import service"},
		tddcheck.LayerDependencyRule{SourceLayer: "adapter", TargetLayer: "repository", Message: "adapter must not import repository"},
		tddcheck.LayerDependencyRule{SourceLayer: "adapter", TargetLayer: "appcmd", Message: "adapter must not import appcmd"},
		tddcheck.LayerDependencyRule{SourceLayer: "adapter", TargetLayer: "adapter", Message: "adapters must not import other adapters"},
		tddcheck.LayerDependencyRule{SourceLayer: "core", TargetLayer: "handler", Message: "core must not import handler"},
		tddcheck.LayerDependencyRule{SourceLayer: "core", TargetLayer: "service", Message: "core must not import service"},
		tddcheck.LayerDependencyRule{SourceLayer: "core", TargetLayer: "repository", Message: "core must not import repository"},
		tddcheck.LayerDependencyRule{SourceLayer: "core", TargetLayer: "adapter", Message: "core must not import adapter"},
		tddcheck.LayerDependencyRule{SourceLayer: "core", TargetLayer: "appcmd", Message: "core must not import appcmd"},
	)
	return config
}
