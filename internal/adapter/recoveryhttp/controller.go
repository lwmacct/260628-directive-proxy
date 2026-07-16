package recoveryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

type Options struct {
	MaxResponseBytes int64
}

type Controller struct {
	client           *http.Client
	transport        *http.Transport
	maxResponseBytes int64
}

func New(options Options) *Controller {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Controller{
		client: &http.Client{
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		transport:        transport,
		maxResponseBytes: options.MaxResponseBytes,
	}
}

func (controller *Controller) Decide(ctx context.Context, spec recovery.ControllerSpec, event recovery.Event) (recovery.Decision, error) {
	if controller == nil || controller.client == nil || spec.URL == nil {
		return recovery.Decision{}, errors.New("recovery controller is unavailable")
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
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.URL.String(), bytes.NewReader(body))
	if err != nil {
		return recovery.Decision{}, err
	}
	request.Header = spec.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
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

func (controller *Controller) Close() error {
	if controller != nil && controller.transport != nil {
		controller.transport.CloseIdleConnections()
	}
	return nil
}
