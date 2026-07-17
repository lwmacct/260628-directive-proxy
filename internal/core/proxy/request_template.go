package proxy

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// RequestTemplate is an immutable snapshot of the original inbound request
// metadata. Bodies are replayed separately; every attempt is rebuilt from this
// snapshot so routing and header mutations cannot leak between attempts.
type RequestTemplate struct {
	Method           string
	URL              *url.URL
	Host             string
	Header           http.Header
	Trailer          http.Header
	TransferEncoding []string
	Proto            string
	ProtoMajor       int
	ProtoMinor       int
	ContentLength    int64
	Close            bool
	IdempotencyKey   string
}

func NewRequestTemplate(req *http.Request) *RequestTemplate {
	if req == nil {
		return nil
	}
	return &RequestTemplate{
		Method:           req.Method,
		URL:              cloneURL(req.URL),
		Host:             req.Host,
		Header:           req.Header.Clone(),
		Trailer:          req.Trailer.Clone(),
		TransferEncoding: append([]string(nil), req.TransferEncoding...),
		Proto:            req.Proto,
		ProtoMajor:       req.ProtoMajor,
		ProtoMinor:       req.ProtoMinor,
		ContentLength:    req.ContentLength,
		Close:            req.Close,
		IdempotencyKey:   req.Header.Get("Idempotency-Key"),
	}
}

func BuildAttemptRequest(template *RequestTemplate, plan *Plan, ctx context.Context, body io.ReadCloser) *http.Request {
	if template == nil || plan == nil || plan.Target == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req := &http.Request{
		Method:           template.Method,
		URL:              cloneURL(plan.Target),
		Proto:            template.Proto,
		ProtoMajor:       template.ProtoMajor,
		ProtoMinor:       template.ProtoMinor,
		Header:           template.Header.Clone(),
		Body:             body,
		ContentLength:    template.ContentLength,
		TransferEncoding: append([]string(nil), template.TransferEncoding...),
		Close:            template.Close,
		Host:             template.Host,
		Trailer:          template.Trailer.Clone(),
	}
	req = req.WithContext(ctx)
	applyPlan(req, template.Header, plan)
	if template.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", template.IdempotencyKey)
	}
	if plan.Proxy != nil {
		req = withRequestProxy(req, plan.Proxy)
	}
	return req
}
