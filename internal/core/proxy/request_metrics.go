package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

type RequestMetrics interface {
	RequestStarted()
	RequestFinished(status int, outcome string, duration time.Duration, requestBodyBytes, responseBodyBytes int64)
}

type metricsResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (writer *metricsResponseWriter) WriteHeader(status int) {
	if writer.wrote {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		writer.ResponseWriter.WriteHeader(status)
		return
	}
	writer.wrote = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *metricsResponseWriter) Write(data []byte) (int, error) {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	written, err := writer.ResponseWriter.Write(data)
	writer.bytes += int64(written)
	return written, err
}

func (writer *metricsResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := writer.ResponseWriter.(io.ReaderFrom); ok {
		written, err := readerFrom.ReadFrom(source)
		writer.bytes += written
		return written, err
	}
	return io.Copy(struct{ io.Writer }{writer}, source)
}

func (writer *metricsResponseWriter) Flush() {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *metricsResponseWriter) FlushError() error {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *metricsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(writer.ResponseWriter).Hijack()
}

func (writer *metricsResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := writer.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (writer *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func requestMetricsOutcome(request *http.Request, status int) string {
	if request != nil && (request.Context().Err() == context.Canceled || request.Context().Err() == context.DeadlineExceeded) {
		return "canceled"
	}
	if status == http.StatusSwitchingProtocols || status >= 200 && status < 400 {
		return "success"
	}
	return "error"
}
