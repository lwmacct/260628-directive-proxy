package proxydirective

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

var ErrInvalidPayload = errors.New("invalid proxy payload")

func Validate(payload Payload) error {
	payload = withProtocolDefaults(payload)
	_, err := NormalizePayload(payload, AssembleOptions{})
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
	case "", string(proxyplan.HeaderModePatch), string(proxyplan.HeaderModeReplace):
		return nil
	default:
		return ErrInvalidPayload
	}
}

func validateLabels(labels map[string]any) error {
	for key := range labels {
		if strings.TrimSpace(key) == "" {
			return ErrInvalidPayload
		}
	}
	return nil
}
