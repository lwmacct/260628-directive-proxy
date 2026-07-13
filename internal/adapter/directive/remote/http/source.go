package remotehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
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

type ResolveRequest struct {
	Protocol string          `json:"protocol"`
	Key      string          `json:"key,omitempty"`
	Request  RequestMetadata `json:"request"`
}

type RequestMetadata struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Host    string              `json:"host"`
	Headers map[string][]string `json:"headers,omitempty"`
}

func New(opts Options) *Source {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
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

func (s *Source) Read(ctx context.Context, spec directive.RemoteSpec, req *http.Request) ([]byte, error) {
	if req == nil {
		return nil, directive.ErrRemoteInvalid
	}
	body, err := json.Marshal(ResolveRequest{
		Protocol: "dproxy.resolve.v1",
		Key:      spec.Key,
		Request: RequestMetadata{
			Method:  req.Method,
			URL:     requestURL(req),
			Host:    req.Host,
			Headers: resolutionHeaders(req.Header, spec.RequestHeaders),
		},
	})
	if err != nil {
		return nil, err
	}
	if s.maxRequestBytes > 0 && int64(len(body)) > s.maxRequestBytes {
		return nil, directive.ErrRemoteMetadataTooBig
	}
	resolverRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resolverRequest.Header.Set("Content-Type", "application/json")
	for name, value := range spec.Headers {
		resolverRequest.Header.Set(name, value)
	}
	response, err := s.client.Do(resolverRequest)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", directive.ErrRemoteUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusNotFound {
		return nil, directive.ErrRemoteNotFound
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", directive.ErrRemoteUnavailable, response.StatusCode)
	}
	value, err := readBounded(response.Body, s.maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", directive.ErrRemoteInvalid, err)
	}
	return value, nil
}

func (s *Source) Close() error {
	if s != nil && s.transport != nil {
		s.transport.CloseIdleConnections()
	}
	return nil
}

func resolutionHeaders(in http.Header, selectors []string) map[string][]string {
	if len(selectors) == 0 {
		return nil
	}
	headers := in.Clone()
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Authorization", "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		headers.Del(name)
	}
	for name := range headers {
		if !matchesHeaderSelector(name, selectors) {
			headers.Del(name)
		}
	}
	return headers
}

func matchesHeaderSelector(name string, selectors []string) bool {
	name = strings.ToLower(name)
	for _, selector := range selectors {
		matched, _ := path.Match(strings.ToLower(selector), name)
		if matched {
			return true
		}
	}
	return false
}

func requestURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	u := *req.URL
	if !u.IsAbs() {
		u.Scheme = "http"
		if req.TLS != nil {
			u.Scheme = "https"
		}
		u.Host = req.Host
	}
	return u.String()
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
