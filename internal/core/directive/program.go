package directive

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func normalizeProgram(source program.Program) (program.Program, error) {
	if len(source) > maxModuleSpecs {
		return nil, ErrInvalidPayload
	}
	result := make(program.Program, len(source))
	seen := make(map[string]struct{}, len(source))
	for index, spec := range source {
		if spec.Scope != module.ScopeExchange && spec.Scope != module.ScopeAttempt {
			return nil, ErrInvalidPayload
		}
		if spec.ID == "" || spec.ID != strings.TrimSpace(spec.ID) || len(spec.ID) > maxModuleNameBytes || !isModuleName(spec.ID) {
			return nil, ErrInvalidPayload
		}
		if spec.Module == "" || spec.Module != strings.TrimSpace(spec.Module) || len(spec.Module) > maxModuleNameBytes || !isModuleName(spec.Module) {
			return nil, ErrInvalidPayload
		}
		if _, exists := seen[spec.ID]; exists {
			return nil, ErrInvalidPayload
		}
		seen[spec.ID] = struct{}{}
		if len(spec.Config) == 0 {
			spec.Config = json.RawMessage(`{}`)
		}
		if len(spec.Config) > maxModuleSpecBytes || !json.Valid(spec.Config) {
			return nil, ErrInvalidPayload
		}
		compact := bytes.NewBuffer(make([]byte, 0, len(spec.Config)))
		if err := json.Compact(compact, spec.Config); err != nil {
			return nil, ErrInvalidPayload
		}
		spec.Config = append([]byte(nil), compact.Bytes()...)
		result[index] = spec
	}
	return result, nil
}
