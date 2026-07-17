package directive

import (
	"context"
	"net/http"
	"net/url"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type HTTPReference struct {
	Endpoint url.URL
	Headers  httpheader.RequestPlan
}

type RedisReference struct {
	Endpoint url.URL
	Key      string
}

type FileReference struct {
	Path string
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

type FileRemoteReader interface {
	Read(context.Context, FileReference) ([]byte, error)
}

type compiledRemote struct {
	backend  string
	endpoint string
	resource string
	http     *HTTPReference
	redis    *RedisReference
	file     *FileReference
}

func compileRemoteSpec(spec RemoteSpec) (compiledRemote, error) {
	if countRemoteBackends(spec) != 1 {
		return compiledRemote{}, ErrInvalidPayload
	}
	switch {
	case spec.HTTP != nil:
		reference, err := compileHTTPReference(*spec.HTTP)
		return compiledRemote{backend: RemoteTypeHTTP, endpoint: spec.HTTP.URL, http: &reference}, err
	case spec.Redis != nil:
		reference, err := compileRedisReference(*spec.Redis)
		return compiledRemote{backend: RemoteTypeRedis, endpoint: spec.Redis.URL, resource: spec.Redis.Key, redis: &reference}, err
	case spec.File != nil:
		reference, err := compileFileReference(*spec.File)
		return compiledRemote{backend: RemoteTypeFile, resource: spec.File.Path, file: &reference}, err
	}
	return compiledRemote{}, ErrInvalidPayload
}

func compileHTTPReference(spec HTTPRemoteSpec) (HTTPReference, error) {
	endpoint, err := url.Parse(spec.URL)
	if err != nil || endpoint.Host == "" || endpoint.Fragment != "" {
		return HTTPReference{}, ErrInvalidPayload
	}
	if (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return HTTPReference{}, ErrInvalidPayload
	}
	headers, err := CompileResolverRequestHeaders(spec.Headers)
	if err != nil {
		return HTTPReference{}, err
	}
	return HTTPReference{Endpoint: *endpoint, Headers: headers}, nil
}

func compileRedisReference(spec RedisRemoteSpec) (RedisReference, error) {
	endpoint, err := url.Parse(spec.URL)
	if err != nil || endpoint.Host == "" || endpoint.Fragment != "" {
		return RedisReference{}, ErrInvalidPayload
	}
	if endpoint.Scheme != "redis" && endpoint.Scheme != "rediss" {
		return RedisReference{}, ErrInvalidPayload
	}
	if _, err := normalizeRemoteKey(spec.Key); err != nil {
		return RedisReference{}, err
	}
	return RedisReference{Endpoint: *endpoint, Key: spec.Key}, nil
}

func compileFileReference(spec FileRemoteSpec) (FileReference, error) {
	path, err := normalizeRemoteFilePath(spec.Path)
	if err != nil {
		return FileReference{}, err
	}
	return FileReference{Path: path}, nil
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
