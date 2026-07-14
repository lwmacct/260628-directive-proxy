package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

var (
	ErrReplayBodyTooLarge = errors.New("proxy request replay body is too large")
	ErrReplayBudgetFull   = errors.New("proxy request replay budget is full")
	ErrActiveCapacity     = errors.New("proxy active request capacity is full")
)

type RetryTransportOptions struct {
	TempDir          string
	MaxBodyBytes     int64
	MaxInflightBytes int64
	ChunkBytes       int
}

type RetryTransport struct {
	base      http.RoundTripper
	options   RetryTransportOptions
	usedBytes atomic.Int64
}

func NewRetryTransport(base http.RoundTripper, options RetryTransportOptions) (*RetryTransport, error) {
	if base == nil {
		base = http.DefaultTransport
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = 32 << 20
	}
	if options.MaxInflightBytes <= 0 {
		options.MaxInflightBytes = 1 << 30
	}
	if options.ChunkBytes <= 0 {
		options.ChunkBytes = 32 << 10
	}
	if options.TempDir != "" {
		if err := os.MkdirAll(options.TempDir, 0o700); err != nil {
			return nil, fmt.Errorf("create retry temp directory: %w", err)
		}
	}
	return &RetryTransport{base: base, options: options}, nil
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil || req == nil {
		return nil, errors.New("proxy retry transport is unavailable")
	}
	session, tracked := proxyrequest.SessionFromContext(req.Context())
	if !tracked || session == nil {
		return t.base.RoundTrip(req)
	}
	replay, err := t.prepareReplay(req, session)
	if err != nil {
		return nil, err
	}
	defer replay.Close()

	for {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithCancel(req.Context())
		attemptRequest := req.Clone(attemptCtx)
		body, bodyErr := replay.Open()
		if bodyErr != nil {
			cancel()
			return nil, bodyErr
		}
		attemptRequest.Body = body
		attemptRequest.GetBody = func() (io.ReadCloser, error) { return replay.Open() }
		attemptRequest.ContentLength = replay.Size()
		attemptRequest.TransferEncoding = nil
		attempt := session.BeginAttempt(attemptRequest, cancel)
		if attempt == 0 {
			cancel()
			return nil, ErrActiveCapacity
		}
		response, roundTripErr := t.base.RoundTrip(attemptRequest)
		action := session.FinishAttempt(attempt, response != nil && roundTripErr == nil, roundTripErr)
		if action == proxyrequest.AttemptRetry && req.Context().Err() == nil {
			cancel()
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			continue
		}
		if roundTripErr != nil || response == nil || response.Body == nil {
			cancel()
			return response, roundTripErr
		}
		response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
		return response, roundTripErr
	}
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	done   atomic.Bool
}

func (b *cancelOnCloseBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish()
	return err
}

func (b *cancelOnCloseBody) finish() {
	if b != nil && b.done.CompareAndSwap(false, true) && b.cancel != nil {
		b.cancel()
	}
}

type replayBody struct {
	transport *RetryTransport
	file      *os.File
	path      string
	size      int64
}

func (t *RetryTransport) prepareReplay(req *http.Request, session proxyrequest.Session) (*replayBody, error) {
	if req.Body == nil || req.Body == http.NoBody {
		digest := sha256.Sum256(nil)
		session.RequestBodyEnd(0, hex.EncodeToString(digest[:]), true)
		return &replayBody{transport: t}, nil
	}
	defer func() { _ = req.Body.Close() }()
	file, err := os.CreateTemp(t.options.TempDir, "dproxy-replay-*")
	if err != nil {
		return nil, fmt.Errorf("create replay body: %w", err)
	}
	replay := &replayBody{transport: t, file: file, path: file.Name()}
	completed := false
	defer func() {
		if !completed {
			_ = replay.Close()
		}
	}()
	hasher := sha256.New()
	buffer := make([]byte, t.options.ChunkBytes)
	for {
		n, readErr := req.Body.Read(buffer)
		if n > 0 {
			if replay.size+int64(n) > t.options.MaxBodyBytes {
				session.RequestBodyEnd(replay.size, hex.EncodeToString(hasher.Sum(nil)), false)
				return nil, ErrReplayBodyTooLarge
			}
			if !t.reserve(int64(n)) {
				session.RequestBodyEnd(replay.size, hex.EncodeToString(hasher.Sum(nil)), false)
				return nil, ErrReplayBudgetFull
			}
			written := 0
			if written, err = file.Write(buffer[:n]); err != nil || written != n {
				t.release(int64(n))
				if err == nil {
					err = io.ErrShortWrite
				}
				return nil, fmt.Errorf("write replay body: wrote %d of %d bytes: %w", written, n, err)
			}
			_, _ = hasher.Write(buffer[:n])
			session.RequestBodyChunk(buffer[:n], replay.size)
			replay.size += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			session.RequestBodyEnd(replay.size, hex.EncodeToString(hasher.Sum(nil)), false)
			return nil, readErr
		}
	}
	session.RequestBodyEnd(replay.size, hex.EncodeToString(hasher.Sum(nil)), true)
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	completed = true
	return replay, nil
}

func (t *RetryTransport) reserve(size int64) bool {
	for {
		current := t.usedBytes.Load()
		if current+size > t.options.MaxInflightBytes {
			return false
		}
		if t.usedBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (t *RetryTransport) release(size int64) { t.usedBytes.Add(-size) }

func (r *replayBody) Size() int64 {
	if r == nil {
		return 0
	}
	return r.size
}

func (r *replayBody) Open() (io.ReadCloser, error) {
	if r == nil || r.file == nil || r.size == 0 {
		return http.NoBody, nil
	}
	return os.Open(r.path)
}

func (r *replayBody) Close() error {
	if r == nil {
		return nil
	}
	if r.file != nil {
		_ = r.file.Close()
	}
	if r.path != "" {
		_ = os.Remove(r.path)
	}
	if r.transport != nil && r.size > 0 {
		r.transport.release(r.size)
		r.size = 0
	}
	return nil
}
