package exchange

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type responseWriter struct {
	http.ResponseWriter
	exchange *Exchange
	wrote    bool
	status   int
}

type observedResponseBody struct {
	source      io.ReadCloser
	attempt     *Attempt
	pending     []byte
	terminalErr error
	done        atomic.Bool
}

func (current *Exchange) WrapResponseWriter(writer http.ResponseWriter) http.ResponseWriter {
	if current == nil || writer == nil {
		return writer
	}
	writer.Header().Set("X-Dproxy-Trace-ID", current.traceID)
	return &responseWriter{ResponseWriter: writer, exchange: current}
}

func (attempt *Attempt) ObserveUpstreamResponse(response *http.Response) {
	if attempt == nil || attempt.exchange == nil || response == nil || response.Body == nil || attempt.closed.Load() {
		return
	}
	current := attempt.exchange
	current.stateMu.Lock()
	metadata := requestmeta.Clone(attempt.metadata)
	current.stateMu.Unlock()
	current.lifecycleMu.Lock()
	attempt.projection = program.NewUpstreamObserver(
		response.Header.Get("Content-Type"), maxProjectedSSEEventBytes, current.requestScope, attempt.scope,
	)
	_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.UpstreamResponseStarted(current.ctx, lifecycle.ResponseStarted{
			StatusCode: response.StatusCode, Header: response.Header.Clone(), Metadata: metadata,
		})
	})
	current.lifecycleMu.Unlock()
	if response.StatusCode != http.StatusSwitchingProtocols {
		response.Body = &observedResponseBody{source: response.Body, attempt: attempt}
	}
}

func (attempt *Attempt) processUpstreamBodyChunk(data []byte) ([]byte, error) {
	if attempt == nil || attempt.exchange == nil || attempt.closed.Load() {
		return nil, context.Canceled
	}
	current := attempt.exchange
	draft := lifecycle.BodyDraft{Data: append([]byte(nil), data...)}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if attempt.closed.Load() {
		return nil, context.Canceled
	}
	if err := current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.UpstreamBodyChunk(current.ctx, lifecycle.BodyChunk{Data: data})
	}); err != nil {
		return nil, err
	}
	if current.requestScope != nil {
		if err := current.requestScope.MutateUpstreamBodyChunk(current.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if attempt.scope != nil {
		if err := attempt.scope.MutateUpstreamBodyChunk(current.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if attempt.projection != nil {
		if err := attempt.projection.Observe(current.ctx, time.Now().UTC(), draft.Data); err != nil {
			return nil, err
		}
	}
	return draft.Data, nil
}

func (attempt *Attempt) finishUpstream(cause error) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	outcome, finishCause := finishCauseForBody(cause)
	attempt.finishLifecycle(outcome, finishCause, cause, true)
	current := attempt.exchange
	current.stateMu.Lock()
	if current.current == attempt {
		current.current = nil
	}
	current.stateMu.Unlock()
}

func (current *Exchange) responseHeaders(status int, headers http.Header) {
	if current == nil {
		return
	}
	current.stateMu.Lock()
	attempt := current.current
	current.stateMu.Unlock()
	current.lifecycleMu.Lock()
	current.responseStatus = status
	current.downstreamAttempt = attempt
	var attemptScope *program.Scope
	if attempt != nil {
		attemptScope = attempt.scope
	}
	current.downstreamProjection = program.NewDownstreamObserver(
		headers.Get("Content-Type"), maxProjectedSSEEventBytes, current.requestScope, attemptScope,
	)
	_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.DownstreamResponseStarted(current.ctx, lifecycle.ResponseStarted{StatusCode: status, Header: headers.Clone()})
	})
	current.lifecycleMu.Unlock()
}

func (current *Exchange) responseBodyChunk(data []byte) {
	if current == nil || len(data) == 0 {
		return
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(current.downstreamAttempt, func(scope *program.Scope) error {
		return scope.DownstreamBodyChunk(current.ctx, lifecycle.BodyChunk{Data: data})
	})
	if current.downstreamProjection != nil {
		_ = current.downstreamProjection.Observe(current.ctx, time.Now().UTC(), data)
	}
}

func (current *Exchange) finishDownstream() {
	if current == nil {
		return
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.downstreamEnded {
		return
	}
	current.downstreamEnded = true
	if current.downstreamProjection != nil {
		_ = current.downstreamProjection.Finish(current.ctx, time.Now().UTC())
		current.downstreamProjection = nil
	}
	_ = current.dispatchLocked(current.downstreamAttempt, func(scope *program.Scope) error {
		return scope.DownstreamBodyEnded(current.ctx, lifecycle.BodyEnded{})
	})
}

func (writer *responseWriter) WriteHeader(status int) {
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
	writer.exchange.responseHeaders(status, writer.Header())
}

func (writer *responseWriter) Write(data []byte) (int, error) {
	if !writer.wrote {
		writer.WriteHeader(http.StatusOK)
	}
	written, err := writer.ResponseWriter.Write(data)
	if written > 0 {
		writer.exchange.responseBodyChunk(data[:written])
	}
	return written, err
}

func (writer *responseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (body *observedResponseBody) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	for {
		if len(body.pending) > 0 {
			n := copy(target, body.pending)
			body.pending = body.pending[n:]
			return n, nil
		}
		if body.terminalErr != nil {
			return 0, body.terminalErr
		}
		buffer := make([]byte, max(len(target), 32<<10))
		n, readErr := body.source.Read(buffer)
		if n > 0 {
			processed, err := body.attempt.processUpstreamBodyChunk(buffer[:n])
			if err != nil {
				body.finish(err)
				return 0, err
			}
			body.pending = processed
		}
		if readErr != nil {
			body.terminalErr = readErr
			body.finish(readErr)
		}
	}
}

func (body *observedResponseBody) Close() error {
	err := body.source.Close()
	cause := err
	if cause == nil {
		cause = io.ErrUnexpectedEOF
	}
	body.finish(cause)
	return err
}

func (body *observedResponseBody) finish(err error) {
	if body != nil && body.done.CompareAndSwap(false, true) {
		body.attempt.finishUpstream(err)
	}
}
