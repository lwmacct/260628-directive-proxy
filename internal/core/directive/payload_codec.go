package directive

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	directivecontract "github.com/lwmacct/260628-directive-proxy/pkg/directive"
)

func Encode(hmacSecret string, payload Payload) (string, error) {
	return EncodeDocument(hmacSecret, Document{Kind: KindInline, Payload: &payload})
}

func EncodeRemote(hmacSecret string, spec RemoteSpec) (string, error) {
	return EncodeDocument(hmacSecret, Document{Kind: KindRemote, Remote: &spec})
}

func EncodeDocument(hmacSecret string, document Document) (string, error) {
	if strings.TrimSpace(hmacSecret) == "" {
		return "", ErrInvalidHMACSecret
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
	return encodeToken(hmacSecret, kind, raw)
}

func Decode(hmacSecret, encoded string) (Document, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ".")
	if len(parts) != 5 || parts[0] != TokenFamily || parts[1] != TokenVersion ||
		(parts[2] != TokenInline && parts[2] != TokenRemote) || parts[3] == "" || parts[4] == "" {
		return Document{}, ErrInvalidPayload
	}
	if strings.TrimSpace(hmacSecret) == "" {
		return Document{}, ErrTokenUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil || len(signature) != sha256.Size {
		return Document{}, ErrTokenUnauthorized
	}
	expected := tokenMAC(hmacSecret, parts[3])
	if !hmac.Equal(signature, expected) {
		return Document{}, ErrTokenUnauthorized
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(raw) == 0 {
		return Document{}, ErrInvalidPayload
	}
	switch parts[2] {
	case TokenInline:
		payload, err := directivecontract.DecodePayload(raw)
		if err != nil {
			return Document{}, err
		}
		return Document{Kind: KindInline, Payload: &payload}, nil
	case TokenRemote:
		spec, err := directivecontract.DecodeRemoteSpec(raw)
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
		payload, err := directivecontract.NormalizePayload(*document.Payload)
		if err != nil {
			return Document{}, err
		}
		document.Payload = &payload
		return document, nil
	case KindRemote:
		if document.Remote == nil || document.Payload != nil {
			return Document{}, ErrInvalidPayload
		}
		spec, err := directivecontract.NormalizeRemoteSpec(*document.Remote)
		if err != nil {
			return Document{}, err
		}
		document.Remote = &spec
		return document, nil
	default:
		return Document{}, ErrInvalidPayload
	}
}

func DecodePayload(raw []byte) (Payload, error) { return directivecontract.DecodePayload(raw) }

func encodeToken(hmacSecret, kind string, raw []byte) (string, error) {
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signingInput := strings.Join([]string{
		TokenFamily,
		TokenVersion,
		kind,
		payload,
	}, ".")
	if strings.TrimSpace(hmacSecret) == "" {
		return "", ErrInvalidHMACSecret
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(tokenMAC(hmacSecret, payload)), nil
}

func tokenMAC(hmacSecret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}
