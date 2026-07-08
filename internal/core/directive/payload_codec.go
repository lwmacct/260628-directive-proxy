package directive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
)

func Encode(payload Payload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return TokenFamily + "." + TokenVersion + "." + base64.RawURLEncoding.EncodeToString(raw), nil
}

func Decode(encoded string) (Payload, error) {
	encoded = strings.TrimSpace(encoded)
	rawPayload, ok := splitToken(encoded)
	if !ok {
		return Payload{}, ErrInvalidPayload
	}
	raw, err := decodeBase64(rawPayload)
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

func splitToken(encoded string) (string, bool) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 3 || parts[0] != TokenFamily || parts[1] != TokenVersion || parts[2] == "" {
		return "", false
	}
	return parts[2], true
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
