package directivehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type Options struct {
	Timeout          time.Duration
	MaxRequestBytes  int64
	MaxResponseBytes int64
}

type Source struct {
	client           *http.Client
	transport        *http.Transport
	maxRequestBytes  int64
	maxResponseBytes int64
}

var _ directive.HTTPRemoteReader = (*Source)(nil)

type resolveRequest struct {
	Protocol string          `json:"protocol"`
	Key      string          `json:"key,omitempty"`
	Request  requestMetadata `json:"request"`
}

type requestMetadata struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Host   string `json:"host"`
}

func New(opts Options) *Source {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	return &Source{
		client: &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		transport:        transport,
		maxRequestBytes:  opts.MaxRequestBytes,
		maxResponseBytes: opts.MaxResponseBytes,
	}
}

func (s *Source) Read(ctx context.Context, reference directive.HTTPReference, request directive.RequestSnapshot) ([]byte, error) {
	if request.Method == "" || request.URL == "" {
		return nil, directive.ErrRemoteInvalid
	}
	body, err := json.Marshal(resolveRequest{
		Protocol: "dproxy.resolve.v1",
		Key:      reference.Key,
		Request: requestMetadata{
			Method: request.Method,
			URL:    request.URL,
			Host:   request.Host,
		},
	})
	if err != nil {
		return nil, err
	}
	if s.maxRequestBytes > 0 && int64(len(body)) > s.maxRequestBytes {
		return nil, directive.ErrRemoteMetadataTooBig
	}
	resolverRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, reference.Endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	baseHeaders := request.Headers.Clone()
	if baseHeaders == nil {
		baseHeaders = make(http.Header)
	}
	baseHeaders.Set("Content-Type", "application/json")
	httpheader.ApplyRequest(resolverRequest, baseHeaders, reference.Headers, httpheader.RequestOptions{})
	response, err := s.client.Do(resolverRequest)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", directive.ErrRemoteUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusNotFound {
		return nil, directive.ErrRemoteNotFound
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", directive.ErrRemoteUnavailable, response.StatusCode)
	}
	value, err := readBounded(response.Body, s.maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", directive.ErrRemoteInvalid, err)
	}
	return value, nil
}

func (s *Source) Close() error {
	if s != nil && s.transport != nil {
		s.transport.CloseIdleConnections()
	}
	return nil
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(reader)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response exceeds limit")
	}
	return data, nil
}
