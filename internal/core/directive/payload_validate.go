package directive

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

var (
	ErrInvalidPayload     = errors.New("invalid proxy payload")
	ErrInvalidTokenSecret = errors.New("invalid directive token secret")
	ErrTokenUnauthorized  = errors.New("directive token authentication failed")
)

func Validate(payload Payload) error {
	_, err := normalizePayload(payload)
	return err
}

func ParseProxy(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, ErrInvalidPayload
	}
	if !strings.EqualFold(parsed.Scheme, "socks5") {
		return nil, ErrInvalidPayload
	}
	if parsed.Host == "" || parsed.Hostname() == "" || parsed.Port() == "" {
		return nil, ErrInvalidPayload
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err != nil {
		return nil, ErrInvalidPayload
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidPayload
	}
	if parsed.User != nil {
		username := strings.TrimSpace(parsed.User.Username())
		password, ok := parsed.User.Password()
		if username == "" || !ok || password == "" {
			return nil, ErrInvalidPayload
		}
	}

	parsed.Scheme = "socks5"
	return parsed, nil
}

func validateHeaderMode(raw string) error {
	switch strings.TrimSpace(raw) {
	case "", string(httpheader.ModePatch), string(httpheader.ModeReplace):
		return nil
	default:
		return ErrInvalidPayload
	}
}
