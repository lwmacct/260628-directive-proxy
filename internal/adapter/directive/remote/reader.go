package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

type Options struct {
	Timeout                  time.Duration
	MaxRequestBytes          int64
	MaxResponseBytes         int64
	RedisClientCacheCapacity int
	RedisClientIdleTimeout   time.Duration
	RedisPoolSize            int
}

type Reader struct {
	httpClient       *http.Client
	httpTransport    *http.Transport
	redisClients     *redisClientCache
	maxRequestBytes  int64
	maxResponseBytes int64
}

type resolveRequest struct {
	Protocol string          `json:"protocol"`
	Key      string          `json:"key,omitempty"`
	Request  requestMetadata `json:"request"`
}

type requestMetadata struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Host    string              `json:"host"`
	Headers map[string][]string `json:"headers,omitempty"`
}

func New(opts Options) *Reader {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Reader{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		httpTransport: transport,
		redisClients: newRedisClientCache(
			opts.RedisClientCacheCapacity,
			opts.RedisClientIdleTimeout,
			opts.RedisPoolSize,
			opts.Timeout,
		),
		maxRequestBytes:  opts.MaxRequestBytes,
		maxResponseBytes: opts.MaxResponseBytes,
	}
}

func (r *Reader) Read(ctx context.Context, spec directive.RemoteSpec, req *http.Request) ([]byte, error) {
	switch spec.Type {
	case directive.RemoteTypeHTTP:
		return r.readHTTP(ctx, spec, req)
	case directive.RemoteTypeRedis:
		return r.readRedis(ctx, spec)
	default:
		return nil, directive.ErrRemoteUnavailable
	}
}

func (r *Reader) readHTTP(ctx context.Context, spec directive.RemoteSpec, req *http.Request) ([]byte, error) {
	if req == nil {
		return nil, directive.ErrRemoteInvalid
	}
	body, err := json.Marshal(resolveRequest{
		Protocol: "dproxy.resolve.v1",
		Key:      spec.Key,
		Request: requestMetadata{
			Method:  req.Method,
			URL:     requestURL(req),
			Host:    req.Host,
			Headers: resolutionHeaders(req.Header),
		},
	})
	if err != nil {
		return nil, err
	}
	if r.maxRequestBytes > 0 && int64(len(body)) > r.maxRequestBytes {
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
	response, err := r.httpClient.Do(resolverRequest)
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
	value, err := readBounded(response.Body, r.maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", directive.ErrRemoteInvalid, err)
	}
	return value, nil
}

func (r *Reader) readRedis(ctx context.Context, spec directive.RemoteSpec) ([]byte, error) {
	client, release, err := r.redisClients.acquire(spec.URL)
	if err != nil {
		return nil, err
	}
	defer release()
	value, err := client.Get(ctx, spec.Key).Bytes()
	if err == redis.Nil {
		return nil, directive.ErrRemoteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", directive.ErrRemoteUnavailable, err)
	}
	if r.maxResponseBytes > 0 && int64(len(value)) > r.maxResponseBytes {
		return nil, fmt.Errorf("%w: response exceeds limit", directive.ErrRemoteInvalid)
	}
	return value, nil
}

func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	if r.httpTransport != nil {
		r.httpTransport.CloseIdleConnections()
	}
	if r.redisClients != nil {
		return r.redisClients.close()
	}
	return nil
}

func resolutionHeaders(in http.Header) map[string][]string {
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
	return headers
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
		return nil, fmt.Errorf("%w: response exceeds limit", directive.ErrRemoteInvalid)
	}
	return data, nil
}
