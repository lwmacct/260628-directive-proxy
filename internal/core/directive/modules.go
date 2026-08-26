package directive

import (
	"encoding/json/jsontext"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

func normalizeModuleSpec(spec module.Spec) (module.Spec, error) {
	if spec.Module == "" || spec.Module != strings.TrimSpace(spec.Module) || len(spec.Module) > maxModuleNameBytes || !isModuleName(spec.Module) {
		return module.Spec{}, ErrInvalidPayload
	}
	if len(spec.Config) == 0 {
		spec.Config = jsontext.Value(`{}`)
	}
	if len(spec.Config) > maxModuleSpecBytes || !spec.Config.IsValid() {
		return module.Spec{}, ErrInvalidPayload
	}
	config := spec.Config.Clone()
	if err := config.Compact(); err != nil {
		return module.Spec{}, ErrInvalidPayload
	}
	spec.Config = config
	return spec, nil
}

func normalizeModules(specs module.Specs) (module.Specs, error) {
	if len(specs) > maxModuleSpecs {
		return nil, ErrInvalidPayload
	}
	result := make(module.Specs, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		normalized, err := normalizeModuleSpec(spec)
		if err != nil {
			return nil, err
		}
		spec = normalized
		if _, exists := seen[spec.Module]; exists {
			return nil, ErrInvalidPayload
		}
		seen[spec.Module] = struct{}{}
		result[index] = spec
	}
	return result, nil
}
