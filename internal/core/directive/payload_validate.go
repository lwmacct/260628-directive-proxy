package directive

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

var (
	ErrInvalidPayload  = errors.New("invalid proxy payload")
	ErrPayloadTooLarge = errors.New("proxy payload is too large")
)

func Validate(payload Payload) error {
	_, err := ToPlan(payload, AssembleOptions{})
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
	case "", string(proxy.HeaderModePatch), string(proxy.HeaderModeReplace):
		return nil
	default:
		return ErrInvalidPayload
	}
}
