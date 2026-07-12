package capture

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/exchange"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

type Observer struct {
	collector exchange.Collector
}

type observation struct {
	collector    exchange.Collector
	ctx          context.Context
	mu           sync.Mutex
	record       exchange.Record
	requestBody  *bodyCapture
	responseBody *bodyCapture
	response     *responseWriter
}

type bodyCapture struct {
	mu       sync.Mutex
	maxBytes int64
	total    int64
	buf      bytes.Buffer
}

type readCloser struct {
	io.ReadCloser
	capture *bodyCapture
}

type responseWriter struct {
	http.ResponseWriter
	capture *bodyCapture
	status  int
	wrote   bool
}

func NewObserver(collector exchange.Collector) *Observer {
	return &Observer{collector: collector}
}

func (o *Observer) Start(req *http.Request) proxy.Observation {
	if o == nil || o.collector == nil || req == nil {
		return nil
	}
	capture, enabled := o.collector.Begin()
	if !enabled {
		return nil
	}
	return &observation{
		collector:    o.collector,
		ctx:          req.Context(),
		requestBody:  newBodyCapture(capture.MaxBodyBytes),
		responseBody: newBodyCapture(capture.MaxBodyBytes),
		record: exchange.Record{
			ID:             capture.ID,
			StartedAt:      time.Now().UTC(),
			Method:         req.Method,
			Host:           req.Host,
			URL:            requestURL(req),
			RequestHeaders: redactHeaders(req.Header),
		},
	}
}

func (o *observation) WrapRequest(req *http.Request) *http.Request {
	if o == nil || req == nil || req.Body == nil || req.Body == http.NoBody {
		return req
	}
	cloned := req.Clone(req.Context())
	cloned.Body = &readCloser{ReadCloser: req.Body, capture: o.requestBody}
	return cloned
}

func (o *observation) WrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	if o == nil || w == nil {
		return w
	}
	o.response = &responseWriter{ResponseWriter: w, capture: o.responseBody}
	return o.response
}

func (o *observation) SetTargetURL(target *url.URL) {
	if o == nil || target == nil {
		return
	}
	o.mu.Lock()
	o.record.TargetURL = target.String()
	o.mu.Unlock()
}

func (o *observation) SetOutboundRequest(req *http.Request) {
	if o == nil || req == nil {
		return
	}
	o.mu.Lock()
	o.record.OutboundRequestHeaders = redactHeaders(req.Header)
	o.mu.Unlock()
}

func (o *observation) Finish() {
	if o == nil || o.collector == nil {
		return
	}
	completedAt := time.Now().UTC()
	o.mu.Lock()
	record := o.record
	o.mu.Unlock()
	record.CompletedAt = completedAt
	record.DurationMillis = completedAt.Sub(record.StartedAt).Milliseconds()
	record.RequestBody = o.requestBody.snapshot()
	record.ResponseBody = o.responseBody.snapshot()
	if o.response != nil {
		record.StatusCode = o.response.statusCode()
		record.ResponseHeaders = redactHeaders(o.response.Header())
	}
	if err := o.collector.Complete(context.WithoutCancel(o.ctx), record); err != nil {
		slog.Error("complete proxy exchange observation", "error", err)
	}
}

func newBodyCapture(maxBytes int64) *bodyCapture {
	if maxBytes < 0 {
		maxBytes = 0
	}
	return &bodyCapture{maxBytes: maxBytes}
}

func (c *bodyCapture) write(p []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += int64(len(p))
	if c.maxBytes == 0 || int64(c.buf.Len()) >= c.maxBytes {
		return
	}
	remaining := c.maxBytes - int64(c.buf.Len())
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	_, _ = c.buf.Write(p)
}

func (c *bodyCapture) snapshot() exchange.Body {
	if c == nil {
		return exchange.Body{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	data := append([]byte(nil), c.buf.Bytes()...)
	body := exchange.Body{Bytes: c.total, CapturedBytes: len(data), Truncated: c.total > int64(len(data))}
	if utf8.Valid(data) {
		body.Text = string(data)
	} else if len(data) > 0 {
		body.Base64 = base64.StdEncoding.EncodeToString(data)
	}
	return body
}

func (r *readCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.capture.write(p[:n])
	}
	return n, err
}

func (w *responseWriter) WriteHeader(statusCode int) {
	if w.wrote {
		return
	}
	if statusCode >= 100 && statusCode < 200 && statusCode != http.StatusSwitchingProtocols {
		w.status = statusCode
		w.ResponseWriter.WriteHeader(statusCode)
		return
	}
	w.status = statusCode
	w.wrote = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.capture.write(p)
	return w.ResponseWriter.Write(p)
}

func (w *responseWriter) statusCode() int {
	if w == nil || !w.wrote {
		return http.StatusOK
	}
	return w.status
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func requestURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	if req.URL.IsAbs() {
		return req.URL.String()
	}
	u := *req.URL
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Host == "" {
		u.Host = req.Host
	}
	return u.String()
}

func redactHeaders(in http.Header) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for name, values := range in {
		key := http.CanonicalHeaderKey(name)
		if shouldRedactHeader(key) {
			out[key] = []string{"<redacted>"}
		} else {
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}

func shouldRedactHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key":
		return true
	default:
		return false
	}
}
