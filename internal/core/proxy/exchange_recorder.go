package proxy

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type ExchangeRecorder struct {
	mu           sync.RWMutex
	enabled      bool
	nextID       uint64
	total        uint64
	capacity     int
	maxBodyBytes int64
	records      []ExchangeRecord
}

type ExchangeSnapshot struct {
	Enabled      bool             `json:"enabled"`
	Capacity     int              `json:"capacity"`
	MaxBodyBytes int64            `json:"max_body_bytes"`
	Total        uint64           `json:"total"`
	Items        []ExchangeRecord `json:"items"`
}

type ExchangeRecord struct {
	ID              uint64               `json:"id"`
	StartedAt       time.Time            `json:"started_at"`
	CompletedAt     time.Time            `json:"completed_at"`
	DurationMillis  int64                `json:"duration_millis"`
	Method          string               `json:"method"`
	Host            string               `json:"host,omitempty"`
	URL             string               `json:"url"`
	TargetURL       string               `json:"target_url,omitempty"`
	StatusCode      int                  `json:"status_code"`
	RequestHeaders  http.Header          `json:"request_headers,omitempty"`
	ResponseHeaders http.Header          `json:"response_headers,omitempty"`
	RequestBody     ExchangeBodySnapshot `json:"request_body"`
	ResponseBody    ExchangeBodySnapshot `json:"response_body"`
}

type ExchangeBodySnapshot struct {
	Text          string `json:"text,omitempty"`
	Base64        string `json:"base64,omitempty"`
	Bytes         int64  `json:"bytes"`
	CapturedBytes int    `json:"captured_bytes"`
	Truncated     bool   `json:"truncated"`
}

type activeExchange struct {
	recorder     *ExchangeRecorder
	mu           sync.Mutex
	record       ExchangeRecord
	requestBody  *bodyCapture
	responseBody *bodyCapture
	response     *captureResponseWriter
}

type bodyCapture struct {
	mu       sync.Mutex
	maxBytes int64
	total    int64
	buf      bytes.Buffer
}

type captureReadCloser struct {
	rc      io.ReadCloser
	capture *bodyCapture
}

type captureResponseWriter struct {
	http.ResponseWriter
	capture *bodyCapture
	status  int
	wrote   bool
}

func NewExchangeRecorder(capacity int, maxBodyBytes int64) *ExchangeRecorder {
	if capacity <= 0 {
		capacity = DefaultExchangeCapacity
	}
	if maxBodyBytes < 0 {
		maxBodyBytes = DefaultExchangeMaxBodyBytes
	}
	return &ExchangeRecorder{
		capacity:     capacity,
		maxBodyBytes: maxBodyBytes,
		records:      make([]ExchangeRecord, 0, capacity),
	}
}

const (
	DefaultExchangeCapacity     = 100
	DefaultExchangeMaxBodyBytes = 64 << 10
)

func (r *ExchangeRecorder) Snapshot(limit int) ExchangeSnapshot {
	if r == nil {
		return ExchangeSnapshot{Enabled: false, Items: []ExchangeRecord{}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.snapshotLocked(limit)
}

func (r *ExchangeRecorder) Configure(enabled bool, capacity int, maxBodyBytes int64) ExchangeSnapshot {
	if r == nil {
		return ExchangeSnapshot{Enabled: false, Items: []ExchangeRecord{}}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.enabled = enabled
	if capacity > 0 && capacity != r.capacity {
		r.resizeLocked(capacity)
	}
	if maxBodyBytes >= 0 {
		r.maxBodyBytes = maxBodyBytes
	}
	return r.snapshotLocked(0)
}

func (r *ExchangeRecorder) Clear() ExchangeSnapshot {
	if r == nil {
		return ExchangeSnapshot{Enabled: false, Items: []ExchangeRecord{}}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID = 0
	r.total = 0
	r.records = make([]ExchangeRecord, 0, r.capacity)
	return r.snapshotLocked(0)
}

func (r *ExchangeRecorder) Get(id uint64) (ExchangeRecord, bool) {
	if r == nil {
		return ExchangeRecord{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := len(r.records) - 1; i >= 0; i-- {
		if r.records[i].ID == id {
			return cloneExchangeRecord(r.records[i]), true
		}
	}
	return ExchangeRecord{}, false
}

func (r *ExchangeRecorder) snapshotLocked(limit int) ExchangeSnapshot {
	if limit <= 0 || limit > len(r.records) {
		limit = len(r.records)
	}
	items := make([]ExchangeRecord, 0, limit)
	for i := len(r.records) - 1; i >= 0 && len(items) < limit; i-- {
		items = append(items, cloneExchangeRecord(r.records[i]))
	}
	return ExchangeSnapshot{
		Enabled:      r.enabled,
		Capacity:     r.capacity,
		MaxBodyBytes: r.maxBodyBytes,
		Total:        r.total,
		Items:        items,
	}
}

func (r *ExchangeRecorder) resizeLocked(capacity int) {
	if capacity <= 0 || capacity == r.capacity {
		return
	}
	if len(r.records) > capacity {
		r.records = append([]ExchangeRecord(nil), r.records[len(r.records)-capacity:]...)
	} else {
		r.records = append([]ExchangeRecord(nil), r.records...)
	}
	r.capacity = capacity
}

func (r *ExchangeRecorder) Start(req *http.Request) *activeExchange {
	if r == nil || req == nil {
		return nil
	}
	r.mu.Lock()
	if !r.enabled {
		r.mu.Unlock()
		return nil
	}
	r.nextID++
	id := r.nextID
	maxBodyBytes := r.maxBodyBytes
	r.mu.Unlock()

	requestBody := newBodyCapture(maxBodyBytes)
	responseBody := newBodyCapture(maxBodyBytes)
	return &activeExchange{
		recorder:     r,
		requestBody:  requestBody,
		responseBody: responseBody,
		record: ExchangeRecord{
			ID:             id,
			StartedAt:      time.Now().UTC(),
			Method:         req.Method,
			Host:           req.Host,
			URL:            requestURL(req),
			RequestHeaders: redactHeaders(req.Header),
		},
	}
}

func (e *activeExchange) WrapRequest(req *http.Request) *http.Request {
	if e == nil || req == nil || req.Body == nil || req.Body == http.NoBody {
		return req
	}
	cloned := req.Clone(req.Context())
	cloned.Body = &captureReadCloser{rc: req.Body, capture: e.requestBody}
	return cloned
}

func (e *activeExchange) WrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	if e == nil || w == nil {
		return w
	}
	e.response = &captureResponseWriter{
		ResponseWriter: w,
		capture:        e.responseBody,
	}
	return e.response
}

func (e *activeExchange) SetTargetURL(target *url.URL) {
	if e == nil || target == nil {
		return
	}
	e.mu.Lock()
	e.record.TargetURL = target.String()
	e.mu.Unlock()
}

func (e *activeExchange) Finish() {
	if e == nil || e.recorder == nil {
		return
	}
	completedAt := time.Now().UTC()

	e.mu.Lock()
	record := e.record
	e.mu.Unlock()

	record.CompletedAt = completedAt
	record.DurationMillis = completedAt.Sub(record.StartedAt).Milliseconds()
	record.RequestBody = e.requestBody.Snapshot()
	record.ResponseBody = e.responseBody.Snapshot()
	if e.response != nil {
		record.StatusCode = e.response.StatusCode()
		record.ResponseHeaders = redactHeaders(e.response.Header())
	}

	e.recorder.add(record)
}

func (r *ExchangeRecorder) add(record ExchangeRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.total++
	if len(r.records) < r.capacity {
		r.records = append(r.records, cloneExchangeRecord(record))
		return
	}
	copy(r.records, r.records[1:])
	r.records[len(r.records)-1] = cloneExchangeRecord(record)
}

func newBodyCapture(maxBytes int64) *bodyCapture {
	if maxBytes < 0 {
		maxBytes = 0
	}
	return &bodyCapture{maxBytes: maxBytes}
}

func (c *bodyCapture) Write(p []byte) {
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

func (c *bodyCapture) Snapshot() ExchangeBodySnapshot {
	if c == nil {
		return ExchangeBodySnapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	data := append([]byte(nil), c.buf.Bytes()...)
	snapshot := ExchangeBodySnapshot{
		Bytes:         c.total,
		CapturedBytes: len(data),
		Truncated:     c.total > int64(len(data)),
	}
	if len(data) == 0 {
		return snapshot
	}
	if utf8.Valid(data) {
		snapshot.Text = string(data)
		return snapshot
	}
	snapshot.Base64 = base64.StdEncoding.EncodeToString(data)
	return snapshot
}

func (c *captureReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 {
		c.capture.Write(p[:n])
	}
	return n, err
}

func (c *captureReadCloser) Close() error {
	return c.rc.Close()
}

func (w *captureResponseWriter) WriteHeader(statusCode int) {
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

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.capture.Write(p)
	return w.ResponseWriter.Write(p)
}

func (w *captureResponseWriter) StatusCode() int {
	if w == nil || !w.wrote {
		return http.StatusOK
	}
	return w.status
}

func (w *captureResponseWriter) Unwrap() http.ResponseWriter {
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

func redactHeaders(in http.Header) http.Header {
	if len(in) == 0 {
		return nil
	}
	out := make(http.Header, len(in))
	for name, values := range in {
		key := http.CanonicalHeaderKey(name)
		if shouldRedactHeader(key) {
			out[key] = []string{"<redacted>"}
			continue
		}
		out[key] = append([]string(nil), values...)
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

func cloneExchangeRecord(record ExchangeRecord) ExchangeRecord {
	record.RequestHeaders = cloneHeader(record.RequestHeaders)
	record.ResponseHeaders = cloneHeader(record.ResponseHeaders)
	return record
}

func cloneHeader(in http.Header) http.Header {
	if len(in) == 0 {
		return nil
	}
	out := make(http.Header, len(in))
	for name, values := range in {
		out[name] = append([]string(nil), values...)
	}
	return out
}
