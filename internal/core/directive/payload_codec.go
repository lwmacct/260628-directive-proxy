package directive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
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
		kind, value = TokenInline, document.Payload
	case KindRemote:
		kind, value = TokenRemote, document.Remote
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
		return ValidateDocument(Document{Kind: KindInline, Payload: &payload})
	case TokenRemote:
		spec, err := decodeRemoteSpec(raw)
		if err != nil {
			return Document{}, err
		}
		return Document{Kind: KindRemote, Remote: &spec}, nil
	default:
		return Document{}, ErrInvalidPayload
	}
}

func ValidateDocument(document Document) (Document, error) {
	switch document.Kind {
	case KindInline:
		if document.Payload == nil || document.Remote != nil || Validate(*document.Payload) != nil {
			return Document{}, ErrInvalidPayload
		}
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
	return normalizeRemoteSpec(spec)
}

func normalizeRemoteSpec(spec RemoteSpec) (RemoteSpec, error) {
	spec.Type = strings.TrimSpace(spec.Type)
	spec.URL = strings.TrimSpace(spec.URL)
	parsed, err := url.Parse(spec.URL)
	if err != nil || parsed.Host == "" {
		return RemoteSpec{}, ErrInvalidPayload
	}
	switch spec.Type {
	case RemoteTypeHTTP:
		if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return RemoteSpec{}, ErrInvalidPayload
		}
	case RemoteTypeRedis:
		if (parsed.Scheme != "redis" && parsed.Scheme != "rediss") || spec.Key == "" || len(spec.Headers) > 0 || len(spec.RequestHeaders) > 0 {
			return RemoteSpec{}, ErrInvalidPayload
		}
	default:
		return RemoteSpec{}, ErrInvalidPayload
	}
	key, err := normalizeRemoteKey(spec.Key, spec.Type == RemoteTypeRedis)
	if err != nil {
		return RemoteSpec{}, err
	}
	spec.Key = key
	normalizedHeaders := make(map[string]string, len(spec.Headers))
	for name, value := range spec.Headers {
		canonicalName := http.CanonicalHeaderKey(strings.TrimSpace(name))
		if !isValidHeaderName(canonicalName) || isForbiddenResolverHeader(canonicalName) || strings.ContainsAny(value, "\r\n") {
			return RemoteSpec{}, ErrInvalidPayload
		}
		if _, exists := normalizedHeaders[canonicalName]; exists {
			return RemoteSpec{}, ErrInvalidPayload
		}
		normalizedHeaders[canonicalName] = value
	}
	if len(normalizedHeaders) > 0 {
		spec.Headers = normalizedHeaders
	} else {
		spec.Headers = nil
	}
	for index, pattern := range spec.RequestHeaders {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || strings.ContainsAny(pattern, "\x00\r\n") {
			return RemoteSpec{}, ErrInvalidPayload
		}
		if _, err := path.Match(strings.ToLower(pattern), "x-header"); err != nil {
			return RemoteSpec{}, ErrInvalidPayload
		}
		spec.RequestHeaders[index] = pattern
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
