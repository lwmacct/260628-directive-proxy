package directive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"uuid"
)

var ErrInvalidPayload = errors.New("invalid directive payload")

const (
	maxMetadataFields             = 16
	maxMetadataNameBytes          = 64
	maxMetadataValueBytes         = 512
	maxMetadataTotalBytes         = 8 << 10
	maxModuleSpecs                = 16
	maxModuleNameBytes            = 64
	maxModuleConfigBytes          = 64 << 10
	maxRecoveryCaptureBodyBytes   = int64(16 << 20)
	maxRecoveryDuration           = 10 * time.Minute
	maxRecoveryControllerHeaders  = 64
	maxRecoveryHeaderValueBytes   = 8 << 10
	defaultRecoveryCaptureBytes   = int64(64 << 10)
	defaultRecoveryMaxElapsed     = 30 * time.Second
	defaultRecoveryControllerWait = 3 * time.Second
)

func ValidatePayload(payload Payload) error {
	_, err := NormalizePayload(payload)
	return err
}

func NormalizePayload(payload Payload) (Payload, error) {
	return normalizePayload(payload, true)
}

func normalizePayload(payload Payload, requireTarget bool) (Payload, error) {
	if !validMetadata(payload.Metadata) {
		return Payload{}, ErrInvalidPayload
	}
	if requireTarget {
		target, err := normalizeTarget(payload.Target)
		if err != nil {
			return Payload{}, err
		}
		payload.Target = target
	} else if payload.Target.BaseURL != "" || payload.Target.ExactURL != "" {
		return Payload{}, ErrInvalidPayload
	}
	proxy, err := normalizeProxy(payload.Proxy)
	if err != nil {
		return Payload{}, err
	}
	payload.Proxy = proxy
	payload.Headers, err = normalizeHeaderPolicy(payload.Headers, true)
	if err != nil {
		return Payload{}, err
	}
	payload.Modules, err = normalizeModules(payload.Modules)
	if err != nil {
		return Payload{}, err
	}
	payload.Recovery, err = normalizeRecovery(payload.Recovery)
	if err != nil {
		return Payload{}, err
	}
	payload.BodyStore, err = normalizeBodyStore(payload.BodyStore)
	if err != nil {
		return Payload{}, err
	}
	payload.Metadata = cloneMetadata(payload.Metadata)
	return payload, nil
}

func ValidateRemoteSpec(spec RemoteSpec) error {
	_, err := NormalizeRemoteSpec(spec)
	return err
}

func NormalizeRemoteSpec(spec RemoteSpec) (RemoteSpec, error) {
	if countRemoteBackends(spec) != 1 {
		return RemoteSpec{}, ErrInvalidPayload
	}
	if spec.UUID != "" {
		id, err := uuid.Parse(strings.TrimSpace(spec.UUID))
		if err != nil || id.String() != strings.ToLower(strings.TrimSpace(spec.UUID)) {
			return RemoteSpec{}, ErrInvalidPayload
		}
		spec.UUID = id.String()
	}
	switch {
	case spec.HTTP != nil:
		value := *spec.HTTP
		endpoint, err := normalizeHTTPRemoteURL(value.URL)
		if err != nil {
			return RemoteSpec{}, err
		}
		value.URL = endpoint
		value.Headers, err = normalizeHeaderPolicy(value.Headers, false)
		if err != nil {
			return RemoteSpec{}, err
		}
		spec.HTTP = &value
	case spec.Redis != nil:
		value := *spec.Redis
		endpoint, err := normalizeRedisRemoteURL(value.URL)
		if err != nil {
			return RemoteSpec{}, err
		}
		value.URL = endpoint
		value.Key, err = normalizeRemoteKey(value.Key)
		if err != nil {
			return RemoteSpec{}, err
		}
		spec.Redis = &value
	case spec.File != nil:
		value := *spec.File
		var err error
		value.Path, err = normalizeRemoteFilePath(value.Path)
		if err != nil {
			return RemoteSpec{}, err
		}
		spec.File = &value
	}
	return spec, nil
}

func normalizeTarget(target TargetSection) (TargetSection, error) {
	target.BaseURL = strings.TrimSpace(target.BaseURL)
	target.ExactURL = strings.TrimSpace(target.ExactURL)
	if (target.BaseURL == "") == (target.ExactURL == "") {
		return TargetSection{}, ErrInvalidPayload
	}
	raw := target.BaseURL
	if raw == "" {
		raw = target.ExactURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || !isHTTPURL(parsed) {
		return TargetSection{}, ErrInvalidPayload
	}
	return target, nil
}

func normalizeProxy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "socks5") || parsed.Host == "" || parsed.Hostname() == "" || parsed.Port() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalidPayload
	}
	if _, _, err = net.SplitHostPort(parsed.Host); err != nil {
		return "", ErrInvalidPayload
	}
	if parsed.User != nil {
		username := strings.TrimSpace(parsed.User.Username())
		password, ok := parsed.User.Password()
		if username == "" || !ok || password == "" {
			return "", ErrInvalidPayload
		}
	}
	parsed.Scheme = "socks5"
	return parsed.String(), nil
}

func validMetadata(values map[string]string) bool {
	if len(values) > maxMetadataFields {
		return false
	}
	total := 0
	for key, value := range values {
		if key == "" || len(key) > maxMetadataNameBytes || key != strings.TrimSpace(key) || value == "" || len(value) > maxMetadataValueBytes || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n") {
			return false
		}
		for index, char := range key {
			if char >= 'a' && char <= 'z' || index > 0 && char >= '0' && char <= '9' || index > 0 && char == '_' {
				continue
			}
			return false
		}
		total += len(key) + len(value)
		if total > maxMetadataTotalBytes {
			return false
		}
	}
	return true
}

func cloneMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func normalizeHeaderPolicy(policy *HeaderPolicy, allowResponse bool) (*HeaderPolicy, error) {
	if policy == nil {
		return nil, nil
	}
	out := *policy
	out.Mutations = make([]HeaderMutation, len(policy.Mutations))
	for index, mutation := range policy.Mutations {
		if mutation.Side != HeaderSideRequest && (mutation.Side != HeaderSideResponse || !allowResponse) {
			return nil, ErrInvalidPayload
		}
		name, glob := strings.TrimSpace(mutation.Name), strings.TrimSpace(mutation.Glob)
		if (name == "") == (glob == "") {
			return nil, ErrInvalidPayload
		}
		if name != "" && !validHeaderName(name) {
			return nil, ErrInvalidPayload
		}
		if glob != "" {
			if _, err := path.Match(strings.ToLower(glob), ""); err != nil {
				return nil, ErrInvalidPayload
			}
		}
		switch mutation.Action {
		case HeaderActionAdd:
			if len(mutation.Values) == 0 {
				return nil, ErrInvalidPayload
			}
		case HeaderActionSet:
			if len(mutation.Values) != 1 {
				return nil, ErrInvalidPayload
			}
		case HeaderActionDel:
			if len(mutation.Values) != 0 {
				return nil, ErrInvalidPayload
			}
		default:
			return nil, ErrInvalidPayload
		}
		for _, value := range mutation.Values {
			if !validHeaderValue(value) {
				return nil, ErrInvalidPayload
			}
		}
		if name != "" && strings.HasPrefix(strings.ToLower(name), "x-dp-") {
			return nil, ErrInvalidPayload
		}
		if mutation.Side == HeaderSideRequest && name != "" && strings.EqualFold(name, "Host") && (mutation.Action == HeaderActionAdd || len(mutation.Values) > 1) {
			return nil, ErrInvalidPayload
		}
		if mutation.Side == HeaderSideResponse && name != "" && protectedResponseHeader(name) {
			return nil, ErrInvalidPayload
		}
		mutation.Name = name
		mutation.Glob = glob
		mutation.Values = append([]string(nil), mutation.Values...)
		out.Mutations[index] = mutation
	}
	return &out, nil
}

func normalizeModules(specs ModuleSpecs) (ModuleSpecs, error) {
	if len(specs) > maxModuleSpecs {
		return nil, ErrInvalidPayload
	}
	out := make(ModuleSpecs, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if spec.Module == "" || spec.Module != strings.TrimSpace(spec.Module) || len(spec.Module) > maxModuleNameBytes || !validModuleName(spec.Module) {
			return nil, ErrInvalidPayload
		}
		if _, exists := seen[spec.Module]; exists {
			return nil, ErrInvalidPayload
		}
		seen[spec.Module] = struct{}{}
		if len(spec.Config) == 0 {
			spec.Config = json.RawMessage(`{}`)
		}
		if len(spec.Config) > maxModuleConfigBytes || !json.Valid(spec.Config) {
			return nil, ErrInvalidPayload
		}
		buffer := bytes.NewBuffer(make([]byte, 0, len(spec.Config)))
		if err := json.Compact(buffer, spec.Config); err != nil {
			return nil, ErrInvalidPayload
		}
		spec.Config = append(json.RawMessage(nil), buffer.Bytes()...)
		out[index] = spec
	}
	return out, nil
}

func normalizeRecovery(spec *RecoverySpec) (*RecoverySpec, error) {
	if spec == nil {
		return nil, nil
	}
	out := *spec
	controller, err := normalizeRecoveryController(out.Controller)
	if err != nil {
		return nil, err
	}
	out.Controller = controller
	if out.Triggers.ResponseHeaderTimeout != "" {
		value, parseErr := normalizeDuration(out.Triggers.ResponseHeaderTimeout, 0)
		if parseErr != nil {
			return nil, parseErr
		}
		out.Triggers.ResponseHeaderTimeout = value.String()
	}
	if out.Triggers.UnexpectedStatus != nil {
		status := *out.Triggers.UnexpectedStatus
		if len(status.Expected) == 0 {
			return nil, ErrInvalidPayload
		}
		status.Expected = append([]RecoveryStatusRangeSpec(nil), status.Expected...)
		sort.Slice(status.Expected, func(i, j int) bool {
			return status.Expected[i].From < status.Expected[j].From || status.Expected[i].From == status.Expected[j].From && status.Expected[i].To < status.Expected[j].To
		})
		lastTo := 0
		for index, item := range status.Expected {
			if item.From < 200 || item.To > 599 || item.From > item.To || index > 0 && item.From <= lastTo {
				return nil, ErrInvalidPayload
			}
			lastTo = item.To
		}
		if status.CaptureBodyBytes == 0 {
			status.CaptureBodyBytes = defaultRecoveryCaptureBytes
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
	maxElapsed, err := normalizeDuration(out.Budget.MaxElapsed, defaultRecoveryMaxElapsed)
	if err != nil {
		return nil, err
	}
	out.Budget.MaxElapsed = maxElapsed.String()
	return &out, nil
}

func normalizeRecoveryController(spec RecoveryControllerSpec) (RecoveryControllerSpec, error) {
	spec.URL = strings.TrimSpace(spec.URL)
	endpoint, err := url.Parse(spec.URL)
	if err != nil || endpoint.Host == "" || !isHTTPURL(endpoint) {
		return RecoveryControllerSpec{}, ErrInvalidPayload
	}
	spec.URL = endpoint.String()
	timeout, err := normalizeDuration(spec.Timeout, defaultRecoveryControllerWait)
	if err != nil {
		return RecoveryControllerSpec{}, err
	}
	spec.Timeout = timeout.String()
	if len(spec.Headers) > maxRecoveryControllerHeaders {
		return RecoveryControllerSpec{}, ErrInvalidPayload
	}
	headers := make(map[string]string, len(spec.Headers))
	for rawName, value := range spec.Headers {
		name := http.CanonicalHeaderKey(strings.TrimSpace(rawName))
		if !validHeaderName(name) || !validHeaderValue(value) || len(value) > maxRecoveryHeaderValueBytes {
			return RecoveryControllerSpec{}, ErrInvalidPayload
		}
		if _, exists := headers[name]; exists {
			return RecoveryControllerSpec{}, ErrInvalidPayload
		}
		headers[name] = value
	}
	if len(headers) == 0 {
		headers = nil
	}
	spec.Headers = headers
	return spec, nil
}

func normalizeBodyStore(spec *BodyStoreSpec) (*BodyStoreSpec, error) {
	if spec == nil {
		return nil, nil
	}
	out := *spec
	if out.MaxBodyBytes != nil && *out.MaxBodyBytes <= 0 || out.ChunkBytes != nil && (*out.ChunkBytes < 4<<10 || *out.ChunkBytes > 1<<20) {
		return nil, ErrInvalidPayload
	}
	for _, raw := range []*string{out.QueueWait, out.ReadTimeout} {
		if raw == nil {
			continue
		}
		value, err := time.ParseDuration(strings.TrimSpace(*raw))
		if err != nil || value < 0 || strings.TrimSpace(*raw) == "" {
			return nil, ErrInvalidPayload
		}
	}
	return &out, nil
}

func normalizeDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		if fallback <= 0 {
			return 0, ErrInvalidPayload
		}
		return fallback, nil
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 || value > maxRecoveryDuration {
		return 0, ErrInvalidPayload
	}
	return value, nil
}

func normalizeHTTPRemoteURL(raw string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.Fragment != "" || endpoint.User != nil || !isHTTPURL(endpoint) {
		return "", ErrInvalidPayload
	}
	return endpoint.String(), nil
}

func normalizeRedisRemoteURL(raw string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.Fragment != "" || endpoint.Scheme != "redis" && endpoint.Scheme != "rediss" {
		return "", ErrInvalidPayload
	}
	return endpoint.String(), nil
}

func normalizeRemoteFilePath(value string) (string, error) {
	if value == "." || value != strings.TrimSpace(value) || len(value) > MaxRemoteFilePathBytes || strings.Contains(value, "\\") || !fs.ValidPath(value) {
		return "", ErrInvalidPayload
	}
	return value, nil
}

func normalizeRemoteKey(value string) (string, error) {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) || value == "" || len(value) > MaxRemoteKeyBytes {
		return "", ErrInvalidPayload
	}
	for _, char := range value {
		if char == 0 || char < 0x20 || char == 0x7f {
			return "", ErrInvalidPayload
		}
	}
	return value, nil
}

func countRemoteBackends(spec RemoteSpec) int {
	count := 0
	if spec.HTTP != nil {
		count++
	}
	if spec.Redis != nil {
		count++
	}
	if spec.File != nil {
		count++
	}
	return count
}

func validModuleName(value string) bool {
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || (char == '-' || char == '.') && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", char) {
			continue
		}
		return false
	}
	return true
}

func validHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '\t' || char >= 0x20 && char != 0x7f {
			continue
		}
		return false
	}
	return true
}

func protectedResponseHeader(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "x-dp-") {
		return true
	}
	switch value {
	case "connection", "content-length", "date", "host", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func isHTTPURL(value *url.URL) bool {
	return value != nil && (strings.EqualFold(value.Scheme, "http") || strings.EqualFold(value.Scheme, "https"))
}
