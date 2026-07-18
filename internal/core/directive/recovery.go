package directive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

const (
	defaultRecoveryMaxElapsed       = 30 * time.Second
	defaultRecoveryCaptureBodyBytes = int64(64 << 10)
	maxRecoveryCaptureBodyBytes     = int64(16 << 20)
	maxRecoveryDuration             = 10 * time.Minute
)

func normalizeRecoverySpec(spec *RecoverySpec) (*RecoverySpec, error) {
	if spec == nil {
		return nil, nil
	}
	out := *spec
	out.Controller.Module = strings.TrimSpace(out.Controller.Module)
	if out.Controller.Module == "" || len(out.Controller.Module) > maxModuleNameBytes || !isModuleName(out.Controller.Module) {
		return nil, ErrInvalidPayload
	}
	if len(out.Controller.Config) == 0 {
		out.Controller.Config = json.RawMessage(`{}`)
	}
	if len(out.Controller.Config) > maxModuleSpecBytes || !json.Valid(out.Controller.Config) {
		return nil, ErrInvalidPayload
	}
	compact := bytes.NewBuffer(make([]byte, 0, len(out.Controller.Config)))
	if err := json.Compact(compact, out.Controller.Config); err != nil {
		return nil, ErrInvalidPayload
	}
	out.Controller.Config = append(json.RawMessage(nil), compact.Bytes()...)

	responseTimeout, err := parseOptionalRecoveryDuration(out.Triggers.ResponseHeaderTimeout)
	if err != nil {
		return nil, err
	}
	if responseTimeout > 0 {
		out.Triggers.ResponseHeaderTimeout = responseTimeout.String()
	}
	if out.Triggers.UnexpectedStatus != nil {
		status := *out.Triggers.UnexpectedStatus
		if len(status.Expected) == 0 {
			return nil, ErrInvalidPayload
		}
		status.Expected = append([]RecoveryStatusRangeSpec(nil), status.Expected...)
		sort.Slice(status.Expected, func(i, j int) bool {
			if status.Expected[i].From == status.Expected[j].From {
				return status.Expected[i].To < status.Expected[j].To
			}
			return status.Expected[i].From < status.Expected[j].From
		})
		lastTo := 0
		for index, item := range status.Expected {
			if item.From < 200 || item.To > 599 || item.From > item.To || index > 0 && item.From <= lastTo {
				return nil, ErrInvalidPayload
			}
			lastTo = item.To
		}
		if status.CaptureBodyBytes == 0 {
			status.CaptureBodyBytes = defaultRecoveryCaptureBodyBytes
		}
		if status.CaptureBodyBytes < 1 || status.CaptureBodyBytes > maxRecoveryCaptureBodyBytes {
			return nil, ErrInvalidPayload
		}
		out.Triggers.UnexpectedStatus = &status
	}
	if out.Triggers.ResponseHeaderTimeout == "" && out.Triggers.UnexpectedStatus == nil && !out.Triggers.TransportError {
		return nil, ErrInvalidPayload
	}
	if out.Budget.MaxRoundTrips < 1 || out.Budget.MaxRoundTrips > 100 {
		return nil, ErrInvalidPayload
	}
	maxElapsed, err := parseRecoveryDuration(out.Budget.MaxElapsed, defaultRecoveryMaxElapsed)
	if err != nil {
		return nil, err
	}
	out.Budget.MaxElapsed = maxElapsed.String()
	return &out, nil
}

func CompileRecovery(spec *RecoverySpec, compiler recovery.Compiler) (*recovery.Policy, error) {
	normalized, err := normalizeRecoverySpec(spec)
	if err != nil || normalized == nil {
		return nil, err
	}
	if compiler == nil {
		return nil, errors.New("recovery controller compiler is unavailable")
	}
	binding, err := compiler.Compile(recovery.ControllerSpec{
		Module: normalized.Controller.Module,
		Config: append(json.RawMessage(nil), normalized.Controller.Config...),
	})
	if err != nil {
		return nil, fmt.Errorf("compile recovery controller %q: %w", normalized.Controller.Module, err)
	}
	if binding == nil {
		return nil, fmt.Errorf("compile recovery controller %q: nil binding", normalized.Controller.Module)
	}
	maxElapsed, _ := time.ParseDuration(normalized.Budget.MaxElapsed)
	responseTimeout, _ := time.ParseDuration(normalized.Triggers.ResponseHeaderTimeout)
	policy := &recovery.Policy{
		ControllerModule: normalized.Controller.Module,
		Controller:       binding,
		Triggers: recovery.TriggerPolicy{
			ResponseHeaderTimeout: responseTimeout,
			TransportError:        normalized.Triggers.TransportError,
		},
		Budget: recovery.Budget{MaxRoundTrips: normalized.Budget.MaxRoundTrips, MaxElapsed: maxElapsed},
	}
	if normalized.Triggers.UnexpectedStatus != nil {
		status := normalized.Triggers.UnexpectedStatus
		compiled := &recovery.UnexpectedStatusPolicy{CaptureBodyBytes: status.CaptureBodyBytes}
		for _, item := range status.Expected {
			compiled.Expected = append(compiled.Expected, recovery.StatusRange{From: item.From, To: item.To})
		}
		policy.Triggers.UnexpectedStatus = compiled
	}
	return policy, nil
}

func parseRecoveryDuration(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 || value > maxRecoveryDuration {
		return 0, ErrInvalidPayload
	}
	return value, nil
}

func parseOptionalRecoveryDuration(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return parseRecoveryDuration(raw, 0)
}
