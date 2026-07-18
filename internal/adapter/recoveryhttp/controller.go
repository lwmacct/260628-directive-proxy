package recoveryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

const (
	Name                = "builtin.recovery"
	defaultTimeout      = 3 * time.Second
	maxTimeout          = 10 * time.Minute
	maxHeaderCount      = 64
	maxHeaderValueBytes = 8 << 10
)

type Options struct {
	MaxResponseBytes int64
	MaxTimeout       time.Duration
}

type Config struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
}

type Definition struct {
	client           *http.Client
	transport        *http.Transport
	maxResponseBytes int64
	maxTimeout       time.Duration
}

type binding struct {
	client           *http.Client
	url              *url.URL
	headers          http.Header
	timeout          time.Duration
	maxResponseBytes int64
}

func New(options Options) *Definition {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Definition{
		client: &http.Client{
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		transport:        transport,
		maxResponseBytes: options.MaxResponseBytes,
		maxTimeout:       options.MaxTimeout,
	}
}

func (*Definition) Name() string { return Name }

func (definition *Definition) CompileController(raw json.RawMessage) (recovery.ControllerBinding, error) {
	if definition == nil || definition.client == nil {
		return nil, errors.New("recovery HTTP controller definition is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return nil, errors.New("recovery HTTP controller config is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("recovery HTTP controller config has trailing data")
	}
	config.URL = strings.TrimSpace(config.URL)
	endpoint, err := url.Parse(config.URL)
	if err != nil || endpoint.Host == "" || !isHTTPURL(endpoint) {
		return nil, errors.New("recovery HTTP controller URL is invalid")
	}
	timeout := defaultTimeout
	if strings.TrimSpace(config.Timeout) != "" {
		timeout, err = time.ParseDuration(config.Timeout)
		if err != nil || timeout <= 0 || timeout > maxTimeout {
			return nil, errors.New("recovery HTTP controller timeout is invalid")
		}
	}
	if definition.maxTimeout > 0 && timeout > definition.maxTimeout {
		timeout = definition.maxTimeout
	}
	headers, err := compileHeaders(config.Headers)
	if err != nil {
		return nil, err
	}
	return &binding{
		client: definition.client, url: endpoint, headers: headers, timeout: timeout,
		maxResponseBytes: definition.maxResponseBytes,
	}, nil
}

func (controller *binding) Decide(ctx context.Context, event recovery.Event) (recovery.Decision, error) {
	if controller == nil || controller.client == nil || controller.url == nil {
		return recovery.Decision{}, errors.New("recovery HTTP controller is unavailable")
	}
	if event.Protocol == "" {
		event.Protocol = recovery.Protocol
	}
	body, err := json.Marshal(event)
	if err != nil {
		return recovery.Decision{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if controller.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, controller.timeout)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, controller.url.String(), bytes.NewReader(body))
	if err != nil {
		return recovery.Decision{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	for name, values := range controller.headers {
		request.Header[name] = append([]string(nil), values...)
	}
	if event.EventID != "" {
		request.Header.Set("Idempotency-Key", event.EventID)
	}
	startedAt := time.Now()
	response, err := controller.client.Do(request)
	if err != nil {
		return recovery.Decision{}, fmt.Errorf("recovery callback failed after %s: %w", time.Since(startedAt), err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return recovery.Decision{}, fmt.Errorf("recovery callback returned status %d", response.StatusCode)
	}
	reader := io.Reader(response.Body)
	if controller.maxResponseBytes > 0 {
		reader = io.LimitReader(response.Body, controller.maxResponseBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return recovery.Decision{}, err
	}
	if controller.maxResponseBytes > 0 && int64(len(data)) > controller.maxResponseBytes {
		return recovery.Decision{}, errors.New("recovery callback response exceeds limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decision recovery.Decision
	if err := decoder.Decode(&decision); err != nil {
		return recovery.Decision{}, errors.New("recovery callback returned an invalid decision")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return recovery.Decision{}, errors.New("recovery callback returned trailing data")
	}
	switch decision.Action {
	case recovery.ActionRetry, recovery.ActionForward, recovery.ActionFail:
	default:
		return recovery.Decision{}, errors.New("recovery callback returned an unsupported action")
	}
	if decision.AfterMS < 0 {
		return recovery.Decision{}, errors.New("recovery callback returned a negative delay")
	}
	return decision, nil
}

func (controller *binding) Observation() recovery.ControllerObservation {
	if controller == nil {
		return recovery.ControllerObservation{}
	}
	endpoint := ""
	if controller.url != nil {
		endpoint = controller.url.String()
	}
	return recovery.ControllerObservation{
		Endpoint: endpoint, Headers: controller.headers.Clone(), Timeout: controller.timeout,
	}
}

func (definition *Definition) Close() error {
	if definition != nil && definition.transport != nil {
		definition.transport.CloseIdleConnections()
	}
	return nil
}

func compileHeaders(source map[string]string) (http.Header, error) {
	if len(source) > maxHeaderCount {
		return nil, errors.New("recovery HTTP controller has too many headers")
	}
	result := make(http.Header, len(source))
	for name, value := range source {
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		if name == "" || strings.ContainsAny(name, " \t\r\n:") || strings.ContainsAny(value, "\r\n") || len(value) > maxHeaderValueBytes {
			return nil, errors.New("recovery HTTP controller header is invalid")
		}
		if _, exists := result[name]; exists {
			return nil, errors.New("recovery HTTP controller header is repeated")
		}
		result.Set(name, value)
	}
	return result, nil
}

func isHTTPURL(value *url.URL) bool {
	return value != nil && (strings.EqualFold(value.Scheme, "http") || strings.EqualFold(value.Scheme, "https"))
}
