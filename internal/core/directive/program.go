package directive

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func normalizeModuleSpec(spec module.Spec) (module.Spec, error) {
	if spec.Module == "" || spec.Module != strings.TrimSpace(spec.Module) || len(spec.Module) > maxModuleNameBytes || !isModuleName(spec.Module) {
		return module.Spec{}, ErrInvalidPayload
	}
	if len(spec.Config) == 0 {
		spec.Config = json.RawMessage(`{}`)
	}
	if len(spec.Config) > maxModuleSpecBytes || !json.Valid(spec.Config) {
		return module.Spec{}, ErrInvalidPayload
	}
	compact := bytes.NewBuffer(make([]byte, 0, len(spec.Config)))
	if err := json.Compact(compact, spec.Config); err != nil {
		return module.Spec{}, ErrInvalidPayload
	}
	spec.Config = append(json.RawMessage(nil), compact.Bytes()...)
	return spec, nil
}

func normalizeProgram(source program.Program) (program.Program, error) {
	if len(source) > maxModuleSpecs {
		return nil, ErrInvalidPayload
	}
	result := make(program.Program, len(source))
	seen := make(map[string]struct{}, len(source))
	for index, spec := range source {
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
