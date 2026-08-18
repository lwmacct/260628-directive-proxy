package directive

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
)

// EncodeRemote signs a normalized RemoteSpec using the public dp.22.remote
// token format. It is intentionally exposed so control-plane components can
// sign at runtime without depending on the proxy's internal packages.
func EncodeRemote(hmacSecret string, spec RemoteSpec) (string, error) {
	if strings.TrimSpace(hmacSecret) == "" {
		return "", ErrInvalidPayload
	}
	normalized, err := NormalizeRemoteSpec(spec)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", ErrInvalidPayload
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	_, _ = mac.Write([]byte(body))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return TokenFamily + "." + TokenVersion + "." + TokenRemote + "." + body + "." + signature, nil
}

// DecodeRemote verifies and decodes a dp.22.remote token.
func DecodeRemote(hmacSecret, encoded string) (RemoteSpec, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ".")
	if len(parts) != 5 || parts[0] != TokenFamily || parts[1] != TokenVersion || parts[2] != TokenRemote || parts[3] == "" || parts[4] == "" || strings.TrimSpace(hmacSecret) == "" {
		return RemoteSpec{}, ErrInvalidPayload
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil || len(signature) != sha256.Size {
		return RemoteSpec{}, ErrInvalidPayload
	}
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	_, _ = mac.Write([]byte(parts[3]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return RemoteSpec{}, ErrInvalidPayload
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return RemoteSpec{}, ErrInvalidPayload
	}
	return DecodeRemoteSpec(raw)
}

func DecodePayload(raw []byte) (Payload, error) {
	payload, err := decodeStrict[Payload](raw)
	if err != nil {
		return Payload{}, err
	}
	return NormalizePayload(payload)
}

// DecodeTemplate decodes a Payload fragment that deliberately omits target.
// Templates are suitable for control-plane policy storage and must be completed
// with a target before they can be consumed by the data plane.
func DecodeTemplate(raw []byte) (Payload, error) {
	canonical, err := CanonicalTemplate(raw)
	if err != nil {
		return Payload{}, err
	}
	return decodeStrict[Payload](canonical)
}

func CanonicalTemplate(raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, ErrInvalidPayload
	}
	if _, exists := fields["target"]; exists {
		return nil, ErrInvalidPayload
	}
	payload, err := decodeStrict[Payload](raw)
	if err != nil {
		return nil, err
	}
	payload, err = normalizePayload(payload, false)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidPayload
	}
	if err = json.Unmarshal(canonical, &fields); err != nil {
		return nil, ErrInvalidPayload
	}
	delete(fields, "target")
	return json.Marshal(fields)
}

func DecodeRemoteSpec(raw []byte) (RemoteSpec, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return RemoteSpec{}, ErrInvalidPayload
	}
	backends := 0
	for _, name := range []string{"http", "redis", "file"} {
		if _, exists := fields[name]; exists {
			backends++
		}
	}
	if backends != 1 {
		return RemoteSpec{}, ErrInvalidPayload
	}
	spec, err := decodeStrict[RemoteSpec](raw)
	if err != nil {
		return RemoteSpec{}, err
	}
	return NormalizeRemoteSpec(spec)
}

func (target *TargetSection) UnmarshalJSON(raw []byte) error {
	type plainTarget TargetSection
	decoded, err := decodeStrict[plainTarget](raw)
	if err != nil {
		return ErrInvalidPayload
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil || len(fields) != 1 {
		return ErrInvalidPayload
	}
	*target = TargetSection(decoded)
	return nil
}

func decodeStrict[T any](raw []byte) (T, error) {
	var value T
	if len(raw) == 0 {
		return value, ErrInvalidPayload
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, ErrInvalidPayload
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value, ErrInvalidPayload
	}
	return value, nil
}
