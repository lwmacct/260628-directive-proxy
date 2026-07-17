package proxy

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type requestProxyContextKey struct{}

func withRequestProxy(req *http.Request, proxyURL *url.URL) *http.Request {
	if req == nil || proxyURL == nil {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), requestProxyContextKey{}, proxyURL))
}

func WithRequestProxy(req *http.Request, proxyURL *url.URL) *http.Request {
	return withRequestProxy(req, proxyURL)
}

func requestProxyFromContext(ctx context.Context) (*url.URL, bool) {
	if ctx == nil {
		return nil, false
	}
	proxyURL, ok := ctx.Value(requestProxyContextKey{}).(*url.URL)
	if !ok || proxyURL == nil {
		return nil, false
	}
	return proxyURL, true
}

type ProxyTransportOptions struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration
}

func NewProxyAwareTransport(base *http.Transport) *http.Transport {
	return NewProxyAwareTransportWithOptions(base, ProxyTransportOptions{})
}

func NewProxyAwareTransportWithOptions(base *http.Transport, opts ProxyTransportOptions) *http.Transport {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport)
	}

	cloned := base.Clone()
	cloned.ForceAttemptHTTP2 = true
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	cloned.Protocols = protocols
	if opts.MaxIdleConns > 0 {
		cloned.MaxIdleConns = opts.MaxIdleConns
	}
	if opts.MaxIdleConnsPerHost > 0 {
		cloned.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	}
	if opts.MaxConnsPerHost > 0 {
		cloned.MaxConnsPerHost = opts.MaxConnsPerHost
	}
	if opts.IdleConnTimeout > 0 {
		cloned.IdleConnTimeout = opts.IdleConnTimeout
	}
	cloned.DisableKeepAlives = false
	cloned.DisableCompression = true
	baseProxy := base.Proxy

	cloned.Proxy = func(req *http.Request) (*url.URL, error) {
		if proxyURL, ok := requestProxyFromContext(req.Context()); ok {
			return proxyURL, nil
		}
		if baseProxy != nil {
			return baseProxy(req)
		}
		return nil, nil
	}
	return cloned
}
