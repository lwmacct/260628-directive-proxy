package directive

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

const (
	defaultRecoveryCallbackTimeout  = 3 * time.Second
	defaultRecoveryMaxElapsed       = 30 * time.Second
	defaultRecoveryCaptureBodyBytes = int64(64 << 10)
	maxRecoveryHeaderCount          = 64
	maxRecoveryHeaderValueBytes     = 8 << 10
	maxRecoveryCaptureBodyBytes     = int64(16 << 20)
	maxRecoveryDuration             = 10 * time.Minute
)

func normalizeRecoverySpec(spec *RecoverySpec) (*RecoverySpec, error) {
	if spec == nil {
		return nil, nil
	}
	out := *spec
	out.Controller.URL = strings.TrimSpace(out.Controller.URL)
	parsed, err := url.Parse(out.Controller.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || !isHTTPURL(parsed) {
		return nil, ErrInvalidPayload
	}
	callbackTimeout, err := parseRecoveryDuration(out.Controller.Timeout, defaultRecoveryCallbackTimeout)
	if err != nil {
		return nil, err
	}
	out.Controller.Timeout = callbackTimeout.String()
	headers, err := normalizeRecoveryHeaders(out.Controller.Headers)
	if err != nil {
		return nil, err
	}
	out.Controller.Headers = headers

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
	if out.Triggers.ResponseHeaderTimeout == "" && out.Triggers.UnexpectedStatus == nil &&
		!out.Triggers.TransportError && !out.Triggers.DirectiveError {
		return nil, ErrInvalidPayload
	}
	if out.Budget.MaxAttempts < 1 || out.Budget.MaxAttempts > 100 {
		return nil, ErrInvalidPayload
	}
	maxElapsed, err := parseRecoveryDuration(out.Budget.MaxElapsed, defaultRecoveryMaxElapsed)
	if err != nil {
		return nil, err
	}
	out.Budget.MaxElapsed = maxElapsed.String()
	return &out, nil
}

func CompileRecovery(spec *RecoverySpec) (*recovery.Policy, error) {
	normalized, err := normalizeRecoverySpec(spec)
	if err != nil || normalized == nil {
		return nil, err
	}
	controllerURL, _ := url.Parse(normalized.Controller.URL)
	callbackTimeout, _ := time.ParseDuration(normalized.Controller.Timeout)
	maxElapsed, _ := time.ParseDuration(normalized.Budget.MaxElapsed)
	responseTimeout, _ := time.ParseDuration(normalized.Triggers.ResponseHeaderTimeout)
	policy := &recovery.Policy{
		Controller: recovery.ControllerSpec{
			URL: controllerURL, Headers: headerFromStringMap(normalized.Controller.Headers), Timeout: callbackTimeout,
		},
		Triggers: recovery.TriggerPolicy{
			ResponseHeaderTimeout: responseTimeout,
			TransportError:        normalized.Triggers.TransportError,
			DirectiveError:        normalized.Triggers.DirectiveError,
		},
		Budget: recovery.Budget{MaxAttempts: normalized.Budget.MaxAttempts, MaxElapsed: maxElapsed},
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

func normalizeRecoveryHeaders(in map[string]string) (map[string]string, error) {
	if len(in) > maxRecoveryHeaderCount {
		return nil, ErrInvalidPayload
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		if !isValidHeaderName(name) || isForbiddenResolverHeader(name) || strings.ContainsAny(value, "\r\n") || len(value) > maxRecoveryHeaderValueBytes {
			return nil, ErrInvalidPayload
		}
		if _, exists := out[name]; exists {
			return nil, ErrInvalidPayload
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func headerFromStringMap(in map[string]string) http.Header {
	out := make(http.Header, len(in))
	for name, value := range in {
		out.Set(name, value)
	}
	return out
}
