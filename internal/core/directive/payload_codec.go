package directive

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
)

const (
	maxRemoteKeyBytes      = 256
	maxRemoteFilePathBytes = 4096
)

func Encode(secret string, payload Payload) (string, error) {
	return EncodeDocument(secret, Document{Kind: KindInline, Payload: &payload})
}

func EncodeRemote(secret string, spec RemoteSpec) (string, error) {
	return EncodeDocument(secret, Document{Kind: KindRemote, Remote: &spec})
}

func EncodeDocument(secret string, document Document) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrInvalidTokenSecret
	}
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
	return encodeToken(secret, kind, raw)
}

func Decode(secret, encoded string) (Document, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ".")
	if len(parts) != 5 || parts[0] != TokenFamily || parts[1] != TokenVersion ||
		(parts[2] != TokenInline && parts[2] != TokenRemote) || parts[3] == "" || parts[4] == "" {
		return Document{}, ErrInvalidPayload
	}
	if strings.TrimSpace(secret) == "" {
		return Document{}, ErrTokenUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil || len(signature) != sha256.Size {
		return Document{}, ErrTokenUnauthorized
	}
	expected := tokenMAC(secret, strings.Join(parts[:4], "."))
	if !hmac.Equal(signature, expected) {
		return Document{}, ErrTokenUnauthorized
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(raw) == 0 {
		return Document{}, ErrInvalidPayload
	}
	switch parts[2] {
	case TokenInline:
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
	compiledMetadata, err := metadata.Compile(payload.Metadata)
	if err != nil {
		return Payload{}, ErrInvalidPayload
	}
	target, err := normalizeTarget(payload.Target)
	if err != nil {
		return Payload{}, err
	}
	modules, err := normalizeModules(payload.Modules)
	if err != nil {
		return Payload{}, err
	}
	recoverySpec, err := normalizeRecoverySpec(payload.Recovery)
	if err != nil {
		return Payload{}, err
	}
	payload.Target = target
	payload.Metadata = compiledMetadata.Map()
	payload.Modules = modules
	payload.Recovery = recoverySpec
	planOnly := payload
	planOnly.Recovery = nil
	if _, err := CompilePayload(planOnly, AssembleOptions{}); err != nil {
		return Payload{}, err
	}
	return payload, nil
}

func normalizeTarget(target TargetSection) (TargetSection, error) {
	target.BaseURL = strings.TrimSpace(target.BaseURL)
	target.ExactURL = strings.TrimSpace(target.ExactURL)
	if (target.BaseURL == "") == (target.ExactURL == "") {
		return TargetSection{}, ErrInvalidPayload
	}
	return target, nil
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

func (target *TargetSection) UnmarshalJSON(raw []byte) error {
	type targetSection TargetSection
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded targetSection
	if err := decoder.Decode(&decoded); err != nil {
		return ErrInvalidPayload
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrInvalidPayload
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 1 {
		return ErrInvalidPayload
	}
	*target = TargetSection(decoded)
	return nil
}

func encodeToken(secret, kind string, raw []byte) (string, error) {
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signingInput := strings.Join([]string{
		TokenFamily,
		TokenVersion,
		kind,
		payload,
	}, ".")
	if strings.TrimSpace(secret) == "" {
		return "", ErrInvalidTokenSecret
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(tokenMAC(secret, signingInput)), nil
}

func tokenMAC(secret, signingInput string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return mac.Sum(nil)
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 1 {
		return RemoteSpec{}, ErrInvalidPayload
	}
	return spec, nil
}

func normalizeRemoteSpec(spec RemoteSpec) (RemoteSpec, error) {
	if countRemoteBackends(spec) != 1 {
		return RemoteSpec{}, ErrInvalidPayload
	}
	switch {
	case spec.HTTP != nil:
		value := *spec.HTTP
		value.URL = strings.TrimSpace(value.URL)
		spec.HTTP = &value
	case spec.Redis != nil:
		value := *spec.Redis
		value.URL = strings.TrimSpace(value.URL)
		key, err := normalizeRemoteKey(value.Key)
		if err != nil {
			return RemoteSpec{}, err
		}
		value.Key = key
		spec.Redis = &value
	case spec.File != nil:
		value := *spec.File
		path, err := normalizeRemoteFilePath(value.Path)
		if err != nil {
			return RemoteSpec{}, err
		}
		value.Path = path
		spec.File = &value
	}
	if _, err := compileRemoteSpec(spec); err != nil {
		return RemoteSpec{}, err
	}
	return spec, nil
}

func normalizeRemoteFilePath(path string) (string, error) {
	if path == "." || path != strings.TrimSpace(path) || len(path) > maxRemoteFilePathBytes ||
		strings.Contains(path, "\\") || !fs.ValidPath(path) {
		return "", ErrInvalidPayload
	}
	return path, nil
}

func normalizeRemoteKey(key string) (string, error) {
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
