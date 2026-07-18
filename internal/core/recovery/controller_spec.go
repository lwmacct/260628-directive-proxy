package recovery

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultControllerTimeout = 3 * time.Second
	MaxControllerTimeout     = 10 * time.Minute
	maxControllerHeaders     = 64
	maxControllerHeaderBytes = 8 << 10
)

func NormalizeControllerSpec(spec ControllerSpec) (ControllerSpec, error) {
	spec.URL = strings.TrimSpace(spec.URL)
	endpoint, err := url.Parse(spec.URL)
	if err != nil || endpoint.Host == "" || !isHTTPControllerURL(endpoint) {
		return ControllerSpec{}, errors.New("recovery controller URL is invalid")
	}
	spec.URL = endpoint.String()

	timeout := DefaultControllerTimeout
	if strings.TrimSpace(spec.Timeout) != "" {
		timeout, err = time.ParseDuration(spec.Timeout)
		if err != nil || timeout <= 0 || timeout > MaxControllerTimeout {
			return ControllerSpec{}, errors.New("recovery controller timeout is invalid")
		}
	}
	spec.Timeout = timeout.String()

	if len(spec.Headers) > maxControllerHeaders {
		return ControllerSpec{}, errors.New("recovery controller has too many headers")
	}
	headers := make(map[string]string, len(spec.Headers))
	for rawName, value := range spec.Headers {
		name := http.CanonicalHeaderKey(strings.TrimSpace(rawName))
		if !isHTTPHeaderName(name) || !isHTTPHeaderValue(value) || len(value) > maxControllerHeaderBytes {
			return ControllerSpec{}, errors.New("recovery controller header is invalid")
		}
		if _, exists := headers[name]; exists {
			return ControllerSpec{}, errors.New("recovery controller header is repeated")
		}
		headers[name] = value
	}
	if len(headers) == 0 {
		headers = nil
	}
	spec.Headers = headers
	return spec, nil
}

func isHTTPControllerURL(value *url.URL) bool {
	return value != nil && (strings.EqualFold(value.Scheme, "http") || strings.EqualFold(value.Scheme, "https"))
}

func isHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", char) {
			continue
		}
		return false
	}
	return true
}

func isHTTPHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '\t' || char >= 0x20 && char != 0x7f {
			continue
		}
		return false
	}
	return true
}
