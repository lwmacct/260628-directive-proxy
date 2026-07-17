package directive

import (
	"context"
	"net/http"
	"net/url"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type HTTPReference struct {
	Endpoint url.URL
	Key      string
	Headers  httpheader.RequestPlan
}

type RedisReference struct {
	Endpoint url.URL
	Key      string
}

type RequestSnapshot struct {
	Method  string
	URL     string
	Host    string
	Headers http.Header
}

type HTTPRemoteReader interface {
	Read(context.Context, HTTPReference, RequestSnapshot) ([]byte, error)
}

type RedisRemoteReader interface {
	Read(context.Context, RedisReference) ([]byte, error)
}

type compiledRemote struct {
	http  *HTTPReference
	redis *RedisReference
}

func compileRemoteSpec(spec RemoteSpec) (compiledRemote, error) {
	endpoint, err := url.Parse(spec.URL)
	if err != nil || endpoint.Host == "" {
		return compiledRemote{}, ErrInvalidPayload
	}
	switch spec.Type {
	case RemoteTypeHTTP:
		if (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
			return compiledRemote{}, ErrInvalidPayload
		}
		headers, err := CompileResolverRequestHeaders(spec.Headers)
		if err != nil {
			return compiledRemote{}, err
		}
		return compiledRemote{http: &HTTPReference{Endpoint: *endpoint, Key: spec.Key, Headers: headers}}, nil
	case RemoteTypeRedis:
		if (endpoint.Scheme != "redis" && endpoint.Scheme != "rediss") || spec.Key == "" || spec.Headers != nil {
			return compiledRemote{}, ErrInvalidPayload
		}
		return compiledRemote{redis: &RedisReference{Endpoint: *endpoint, Key: spec.Key}}, nil
	default:
		return compiledRemote{}, ErrInvalidPayload
	}
}

func snapshotRequest(req *http.Request) RequestSnapshot {
	if req == nil {
		return RequestSnapshot{}
	}
	return RequestSnapshot{
		Method:  req.Method,
		URL:     absoluteRequestURL(req),
		Host:    req.Host,
		Headers: req.Header.Clone(),
	}
}

func absoluteRequestURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	value := *req.URL
	if !value.IsAbs() {
		value.Scheme = "http"
		if req.TLS != nil {
			value.Scheme = "https"
		}
		value.Host = req.Host
	}
	return value.String()
}
