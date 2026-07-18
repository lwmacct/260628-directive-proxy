package exchange

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

type responseWriter struct {
	http.ResponseWriter
	exchange *Exchange
	wrote    bool
	status   int
}

type observedResponseBody struct {
	source      io.ReadCloser
	roundTrip   *RoundTrip
	pending     []byte
	terminalErr error
	done        atomic.Bool
}

func (current *Exchange) WrapResponseWriter(writer http.ResponseWriter) http.ResponseWriter {
	if current == nil || writer == nil {
		return writer
	}
	writer.Header().Set("X-Dp-Trace-ID", current.traceID)
	return &responseWriter{ResponseWriter: writer, exchange: current}
}

func (roundTrip *RoundTrip) ObserveUpstreamResponse(response *http.Response) {
	if roundTrip == nil || roundTrip.exchange == nil || response == nil || response.Body == nil || roundTrip.closed.Load() {
		return
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	roundTrip.projection = program.NewUpstreamObserver(
		response.Header.Get("Content-Type"), maxProjectedSSEEventBytes, current.activeProgram(roundTrip),
	)
	_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.UpstreamResponseStarted(current.ctx, lifecycle.ResponseStarted{
			StatusCode: response.StatusCode, Header: response.Header.Clone(),
		})
	})
	current.lifecycleMu.Unlock()
	if response.StatusCode != http.StatusSwitchingProtocols {
		response.Body = &observedResponseBody{source: response.Body, roundTrip: roundTrip}
	}
}

func (roundTrip *RoundTrip) processUpstreamBodyChunk(data []byte) ([]byte, error) {
	if roundTrip == nil || roundTrip.exchange == nil || roundTrip.closed.Load() {
		return nil, context.Canceled
	}
	current := roundTrip.exchange
	draft := lifecycle.BodyDraft{Data: append([]byte(nil), data...)}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if roundTrip.closed.Load() {
		return nil, context.Canceled
	}
	if err := current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.UpstreamBodyChunk(current.ctx, lifecycle.BodyChunk{Data: data})
	}); err != nil {
		return nil, err
	}
	if active := current.activeProgram(roundTrip); active != nil {
		if err := active.MutateUpstreamBodyChunk(current.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if roundTrip.projection != nil {
		if err := roundTrip.projection.Observe(current.ctx, time.Now().UTC(), draft.Data); err != nil {
			return nil, err
		}
	}
	return draft.Data, nil
}

func (roundTrip *RoundTrip) finishUpstream(cause error) {
	if roundTrip == nil || roundTrip.exchange == nil {
		return
	}
	outcome, finishCause := finishCauseForBody(cause)
	roundTrip.finishLifecycle(outcome, finishCause, cause, true)
	current := roundTrip.exchange
	current.stateMu.Lock()
	if current.current == roundTrip {
		current.current = nil
	}
	current.stateMu.Unlock()
}

func (current *Exchange) responseHeaders(status int, headers http.Header) {
	if current == nil {
		return
	}
	current.stateMu.Lock()
	roundTrip := current.current
	current.stateMu.Unlock()
	current.lifecycleMu.Lock()
	current.responseStatus = status
	current.downstreamRoundTrip = roundTrip
	current.downstreamProjection = program.NewDownstreamObserver(
		headers.Get("Content-Type"), maxProjectedSSEEventBytes, current.activeProgram(roundTrip),
	)
	_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.DownstreamResponseStarted(current.ctx, lifecycle.ResponseStarted{StatusCode: status, Header: headers.Clone()})
	})
	current.lifecycleMu.Unlock()
}

func (current *Exchange) responseBodyChunk(data []byte) {
	if current == nil || len(data) == 0 {
		return
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(current.downstreamRoundTrip, func(active *program.ScopeSet) error {
		return active.DownstreamBodyChunk(current.ctx, lifecycle.BodyChunk{Data: data})
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
	_ = current.dispatchLocked(current.downstreamRoundTrip, func(active *program.ScopeSet) error {
		return active.DownstreamBodyEnded(current.ctx, lifecycle.BodyEnded{})
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
			processed, err := body.roundTrip.processUpstreamBodyChunk(buffer[:n])
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
		body.roundTrip.finishUpstream(err)
	}
}
