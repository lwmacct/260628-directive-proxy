package directive

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func normalizeProgram(source program.Program, allowRequest, allowAttempt bool) (program.Program, error) {
	if !allowRequest && len(source.Request) > 0 || !allowAttempt && len(source.Attempt) > 0 {
		return program.Program{}, ErrInvalidPayload
	}
	request, err := normalizeModuleSpecs(source.Request)
	if err != nil {
		return program.Program{}, err
	}
	attempt, err := normalizeModuleSpecs(source.Attempt)
	if err != nil {
		return program.Program{}, err
	}
	return program.Program{Request: request, Attempt: attempt}, nil
}

func normalizeModuleSpecs(specs []program.Spec) ([]program.Spec, error) {
	if len(specs) > maxModuleSpecs {
		return nil, ErrInvalidPayload
	}
	result := make([]program.Spec, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
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
