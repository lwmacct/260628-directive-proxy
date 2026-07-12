package directive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

const maxRedisKeyBytes = 256

type Token struct {
	Kind     string
	Payload  []byte
	RedisKey string
}

func Encode(payload Payload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return encodeToken(TokenInline, raw), nil
}

func EncodeRedisKey(key string) (string, error) {
	key, err := normalizeRedisKey(key)
	if err != nil {
		return "", err
	}
	return encodeToken(TokenRedis, []byte(key)), nil
}

func Decode(encoded string) (Token, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ".")
	if len(parts) != 4 || parts[0] != TokenFamily || parts[1] != TokenVersion || parts[3] == "" {
		return Token{}, ErrInvalidPayload
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(raw) == 0 {
		return Token{}, ErrInvalidPayload
	}
	switch parts[2] {
	case TokenInline:
		return Token{Kind: TokenInline, Payload: raw}, nil
	case TokenRedis:
		key, err := normalizeRedisKey(string(raw))
		if err != nil {
			return Token{}, err
		}
		return Token{Kind: TokenRedis, RedisKey: key}, nil
	default:
		return Token{}, ErrInvalidPayload
	}
}

func DecodePayload(raw []byte) (Payload, error) {
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

func encodeToken(kind string, raw []byte) string {
	return strings.Join([]string{
		TokenFamily,
		TokenVersion,
		kind,
		base64.RawURLEncoding.EncodeToString(raw),
	}, ".")
}

func normalizeRedisKey(key string) (string, error) {
	if !utf8.ValidString(key) || key != strings.TrimSpace(key) || key == "" || len(key) > maxRedisKeyBytes {
		return "", ErrInvalidPayload
	}
	for _, char := range key {
		if char == 0 || char < 0x20 || char == 0x7f {
			return "", ErrInvalidPayload
		}
	}
	return key, nil
}
