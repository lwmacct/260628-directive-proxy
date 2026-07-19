package bodystore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
)

type Observer struct {
	Chunk func(offset int64, data []byte) error
	End   func(Result)
}

type Result struct {
	Total    int64
	SHA256   string
	Complete bool
	Err      error
}

type Store struct {
	controller  *Controller
	reservation *Reservation
	expected    int64
	maxBody     int64
	chunkBytes  int
	observer    Observer
	ctx         context.Context
	cancel      context.CancelFunc
	source      io.ReadCloser

	mu         sync.Mutex
	notify     chan struct{}
	done       chan struct{}
	chunks     [][]byte
	size       int64
	refs       int64
	ingesting  bool
	retired    bool
	discarding bool
	complete   bool
	cleaned    bool
	result     Result

	ownerOnce  sync.Once
	closeOnce  sync.Once
	sourceOnce sync.Once
}

type Reader struct {
	store  *Store
	ctx    context.Context
	offset int64
	once   sync.Once
}

func Empty(observer Observer) *Store {
	store := &Store{observer: observer, notify: make(chan struct{}), done: make(chan struct{}), refs: 1}
	result := Result{SHA256: emptyDigest(), Complete: true}
	if observer.End != nil {
		observer.End(result)
	}
	store.finish(result)
	return store
}

func (c *Controller) Stream(ctx context.Context, source io.ReadCloser, expected int64, observer Observer, options ...StreamOptions) (*Store, error) {
	var configured StreamOptions
	if len(options) > 0 {
		configured = options[0]
	}
	return c.stream(ctx, source, expected, observer, configured, nil)
}

func (c *Controller) StreamWithReservation(ctx context.Context, source io.ReadCloser, expected int64, observer Observer, options StreamOptions, reservation *Reservation) (*Store, error) {
	return c.stream(ctx, source, expected, observer, options, reservation)
}

func (c *Controller) stream(ctx context.Context, source io.ReadCloser, expected int64, observer Observer, streamOptions StreamOptions, reservation *Reservation) (*Store, error) {
	if c == nil {
		return nil, ErrStoreCapacity
	}
	if streamOptions.MaxBodyBytes == 0 {
		streamOptions.MaxBodyBytes = c.config.MaxBodyBytes
	}
	if streamOptions.MaxBodyBytes <= 0 {
		streamOptions.MaxBodyBytes = c.config.MaxBodyBytes
	}
	if streamOptions.ChunkBytes <= 0 {
		streamOptions.ChunkBytes = c.config.ChunkBytes
	}
	if streamOptions.MaxBodyBytes <= 0 {
		return nil, ErrBodyTooLarge
	}
	if expected > streamOptions.MaxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	reserveSize := streamOptions.MaxBodyBytes
	if expected >= 0 {
		reserveSize = expected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if reservation == nil {
		var err error
		reservation, err = c.admit(ctx, reserveSize, streamOptions.QueueWait)
		if err != nil {
			return nil, err
		}
	} else if reservation.Size() < reserveSize {
		reservation.Close()
		return nil, ErrStoreCapacity
	}
	streamCtx, cancel := context.WithCancel(ctx)
	store := &Store{
		controller: c, reservation: reservation, expected: expected, maxBody: streamOptions.MaxBodyBytes,
		chunkBytes: streamOptions.ChunkBytes, observer: observer, ctx: streamCtx, cancel: cancel,
		source: source, notify: make(chan struct{}), done: make(chan struct{}), refs: 1,
	}
	if source == nil {
		reservation.Close()
		store.reservation = nil
		result := Result{SHA256: emptyDigest(), Complete: true}
		if observer.End != nil {
			observer.End(result)
		}
		store.finish(result)
		return store, nil
	}
	store.mu.Lock()
	store.refs++
	store.ingesting = true
	store.mu.Unlock()
	go store.ingest()
	return store, nil
}

func emptyDigest() string {
	digest := sha256.Sum256(nil)
	return hex.EncodeToString(digest[:])
}

func (s *Store) Open(ctx context.Context) (io.ReadCloser, error) {
	if s == nil {
		return nil, ErrStoreClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleaned {
		return nil, ErrStoreClosed
	}
	if s.retired {
		return nil, ErrStoreRetired
	}
	s.refs++
	return &Reader{store: s, ctx: ctx}, nil
}

func (s *Store) Retire() {
	if s == nil {
		return
	}
	s.ownerOnce.Do(func() {
		s.mu.Lock()
		s.retired = true
		s.releaseRefLocked()
		s.mu.Unlock()
	})
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.Retire()
		if s.cancel != nil {
			s.cancel()
		}
		s.closeSource()
		<-s.done
	})
	return nil
}

func (s *Store) Wait(ctx context.Context) (Result, error) {
	if s == nil {
		return Result{}, ErrStoreClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		s.mu.Lock()
		result := s.result
		s.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (s *Store) Size() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	size := s.size
	s.mu.Unlock()
	return size
}

func (s *Store) ContentLength() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	expected, complete, size := s.expected, s.complete, s.size
	s.mu.Unlock()
	if expected >= 0 {
		return expected
	}
	if complete {
		return size
	}
	return -1
}

func (s *Store) ingest() {
	hasher := sha256.New()
	var total int64
	var terminalErr error
	defer func() {
		complete := terminalErr == nil
		digest := hex.EncodeToString(hasher.Sum(nil))
		result := Result{Total: total, SHA256: digest, Complete: complete, Err: terminalErr}
		if s.observer.End != nil {
			s.observer.End(result)
		}
		s.finish(result)
		s.closeSource()
		s.mu.Lock()
		s.ingesting = false
		s.releaseRefLocked()
		s.mu.Unlock()
	}()

	for {
		if err := s.ctx.Err(); err != nil {
			terminalErr = err
			return
		}
		buffer := make([]byte, s.chunkBytes)
		n, readErr := s.source.Read(buffer)
		if n > 0 {
			data := buffer[:n:n]
			if s.expected >= 0 && total+int64(n) > s.expected {
				terminalErr = fmt.Errorf("request body exceeds declared content length: %w", io.ErrUnexpectedEOF)
				return
			}
			if total+int64(n) > s.maxBody {
				terminalErr = ErrBodyTooLarge
				return
			}
			if s.observer.Chunk != nil {
				if err := s.observer.Chunk(total, data); err != nil {
					terminalErr = err
					return
				}
			}
			_, _ = hasher.Write(data)
			s.append(data)
			total += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if s.expected >= 0 && total != s.expected {
					terminalErr = io.ErrUnexpectedEOF
				}
				return
			}
			if err := s.ctx.Err(); err != nil {
				terminalErr = err
			} else {
				terminalErr = readErr
			}
			return
		}
	}
}

func (s *Store) append(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleaned || s.discarding {
		return
	}
	s.chunks = append(s.chunks, data)
	s.size += int64(len(data))
	s.signalLocked()
}

func (s *Store) finish(result Result) {
	s.mu.Lock()
	if !s.complete {
		s.result = result
		s.complete = true
		s.signalLocked()
		close(s.done)
	}
	s.mu.Unlock()
}

func (s *Store) signalLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func (s *Store) closeSource() {
	if s == nil {
		return
	}
	s.sourceOnce.Do(func() {
		if s.source != nil {
			_ = s.source.Close()
		}
	})
}

func (s *Store) releaseRef() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.releaseRefLocked()
	s.mu.Unlock()
}

func (s *Store) releaseRefLocked() {
	if s.refs > 0 {
		s.refs--
	}
	if s.retired && s.ingesting && s.refs == 1 && !s.discarding {
		s.chunks = nil
		if s.reservation != nil {
			s.reservation.Close()
			s.reservation = nil
		}
		s.discarding = true
	}
	if s.refs == 0 {
		s.cleanupLocked()
	}
}

func (s *Store) cleanupLocked() {
	if s.cleaned {
		return
	}
	s.chunks = nil
	if s.reservation != nil {
		s.reservation.Close()
		s.reservation = nil
	}
	s.cleaned = true
}

func (r *Reader) Read(data []byte) (int, error) {
	if r == nil || r.store == nil {
		return 0, ErrStoreClosed
	}
	if len(data) == 0 {
		return 0, nil
	}
	for {
		store := r.store
		store.mu.Lock()
		if r.offset < store.size {
			available := store.size - r.offset
			if int64(len(data)) > available {
				data = data[:available]
			}
			n := store.readAtLocked(data, r.offset)
			r.offset += int64(n)
			store.mu.Unlock()
			return n, nil
		}
		if store.complete {
			err := store.result.Err
			store.mu.Unlock()
			_ = r.Close()
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		notify := store.notify
		store.mu.Unlock()
		select {
		case <-notify:
		case <-r.ctx.Done():
			err := r.ctx.Err()
			_ = r.Close()
			return 0, err
		}
	}
}

func (s *Store) readAtLocked(data []byte, offset int64) int {
	remainingOffset := offset
	written := 0
	for _, chunk := range s.chunks {
		if remainingOffset >= int64(len(chunk)) {
			remainingOffset -= int64(len(chunk))
			continue
		}
		n := copy(data[written:], chunk[remainingOffset:])
		written += n
		remainingOffset = 0
		if written == len(data) {
			break
		}
	}
	return written
}

func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		if r.store != nil {
			r.store.releaseRef()
			r.store = nil
		}
	})
	return nil
}
