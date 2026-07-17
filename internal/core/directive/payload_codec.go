package directive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

const maxRemoteKeyBytes = 256

func Encode(payload Payload) (string, error) {
	return EncodeDocument(Document{Kind: KindInline, Payload: &payload})
}

func EncodeRemote(spec RemoteSpec) (string, error) {
	return EncodeDocument(Document{Kind: KindRemote, Remote: &spec})
}

func EncodeDocument(document Document) (string, error) {
	document, err := ValidateDocument(document)
	if err != nil {
		return "", err
	}
	var kind string
	var value any
	switch document.Kind {
	case KindInline:
		kind, value = TokenInline, *document.Payload
	case KindRemote:
		kind, value = TokenRemote, *document.Remote
	default:
		return "", ErrInvalidPayload
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return encodeToken(kind, raw), nil
}

func Decode(encoded string) (Document, error) {
	return DecodeWithOptions(encoded, DecodeOptions{})
}

type DecodeOptions struct {
	MaxInlineBytes int64
}

func DecodeWithOptions(encoded string, opts DecodeOptions) (Document, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ".")
	if len(parts) != 4 || parts[0] != TokenFamily || parts[1] != TokenVersion || parts[3] == "" {
		return Document{}, ErrInvalidPayload
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(raw) == 0 {
		return Document{}, ErrInvalidPayload
	}
	switch parts[2] {
	case TokenInline:
		if opts.MaxInlineBytes > 0 && int64(len(raw)) > opts.MaxInlineBytes {
			return Document{}, ErrPayloadTooLarge
		}
		payload, err := DecodePayload(raw)
		if err != nil {
			return Document{}, err
		}
		return Document{Kind: KindInline, Payload: &payload}, nil
	case TokenRemote:
		spec, err := decodeRemoteSpec(raw)
		if err != nil {
			return Document{}, err
		}
		return ValidateDocument(Document{Kind: KindRemote, Remote: &spec})
	default:
		return Document{}, ErrInvalidPayload
	}
}

func ValidateDocument(document Document) (Document, error) {
	switch document.Kind {
	case KindInline:
		if document.Payload == nil || document.Remote != nil {
			return Document{}, ErrInvalidPayload
		}
		payload, err := normalizePayload(*document.Payload)
		if err != nil {
			return Document{}, err
		}
		document.Payload = &payload
		return document, nil
	case KindRemote:
		if document.Remote == nil || document.Payload != nil {
			return Document{}, ErrInvalidPayload
		}
		spec, err := normalizeRemoteSpec(*document.Remote)
		if err != nil {
			return Document{}, err
		}
		document.Remote = &spec
		return document, nil
	default:
		return Document{}, ErrInvalidPayload
	}
}

func normalizePayload(payload Payload) (Payload, error) {
	program, err := normalizeProgram(payload.Program, true, true)
	if err != nil {
		return Payload{}, err
	}
	recoverySpec, err := normalizeRecoverySpec(payload.Recovery)
	if err != nil {
		return Payload{}, err
	}
	payload.Program = program
	payload.Recovery = recoverySpec
	if _, err := ToPlan(payload, AssembleOptions{}); err != nil {
		return Payload{}, err
	}
	return payload, nil
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
	return normalizePayload(payload)
}

func encodeToken(kind string, raw []byte) string {
	return strings.Join([]string{
		TokenFamily,
		TokenVersion,
		kind,
		base64.RawURLEncoding.EncodeToString(raw),
	}, ".")
}

func decodeRemoteSpec(raw []byte) (RemoteSpec, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var spec RemoteSpec
	if err := decoder.Decode(&spec); err != nil {
		return RemoteSpec{}, ErrInvalidPayload
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return RemoteSpec{}, ErrInvalidPayload
	}
	return spec, nil
}

func normalizeRemoteSpec(spec RemoteSpec) (RemoteSpec, error) {
	spec.Type = strings.TrimSpace(spec.Type)
	spec.URL = strings.TrimSpace(spec.URL)
	if spec.Type != RemoteTypeHTTP && spec.Type != RemoteTypeRedis {
		return RemoteSpec{}, ErrInvalidPayload
	}
	key, err := normalizeRemoteKey(spec.Key, spec.Type == RemoteTypeRedis)
	if err != nil {
		return RemoteSpec{}, err
	}
	spec.Key = key
	if _, err := compileRemoteSpec(spec); err != nil {
		return RemoteSpec{}, err
	}
	return spec, nil
}

func normalizeRemoteKey(key string, required bool) (string, error) {
	if key == "" && !required {
		return "", nil
	}
	if !utf8.ValidString(key) || key != strings.TrimSpace(key) || key == "" || len(key) > maxRemoteKeyBytes {
		return "", ErrInvalidPayload
	}
	for _, char := range key {
		if char == 0 || char < 0x20 || char == 0x7f {
			return "", ErrInvalidPayload
		}
	}
	return key, nil
}

func isForbiddenResolverHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "content-length", "content-type", "connection", "proxy-connection", "keep-alive", "transfer-encoding", "upgrade", "trailer", "te":
		return true
	default:
		return false
	}
}
