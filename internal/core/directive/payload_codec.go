package directive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
)

func Encode(payload Payload) (string, error) {
	payload = withProtocolDefaults(payload)
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func Decode(encoded string) (Payload, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, TokenPrefix) {
		return Payload{}, ErrInvalidPayload
	}
	raw, err := decodeBase64(strings.TrimPrefix(encoded, TokenPrefix))
	if err != nil {
		return Payload{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var payload Payload
	if err := decoder.Decode(&payload); err != nil {
		return Payload{}, ErrInvalidPayload
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Payload{}, ErrInvalidPayload
	}
	return payload, nil
}

func withProtocolDefaults(payload Payload) Payload {
	if payload.Version == 0 {
		payload.Version = PayloadVersion
	}
	if strings.TrimSpace(payload.Kind) == "" {
		payload.Kind = PayloadKind
	}
	return payload
}

func decodeBase64(raw string) ([]byte, error) {
	if raw == "" {
		return nil, ErrInvalidPayload
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	return nil, ErrInvalidPayload
}
