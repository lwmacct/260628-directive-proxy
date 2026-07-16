package bodystore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
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
	controller *Controller
	expected   int64
	observer   Observer
	ctx        context.Context
	cancel     context.CancelFunc
	source     io.ReadCloser

	mu          sync.Mutex
	notify      chan struct{}
	done        chan struct{}
	chunks      [][]byte
	file        *os.File
	filePath    string
	size        int64
	memoryBytes int64
	diskBytes   int64
	refs        int64
	ingesting   bool
	retired     bool
	discarding  bool
	complete    bool
	cleaned     bool
	result      Result

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
	store := &Store{
		observer: observer,
		notify:   make(chan struct{}),
		done:     make(chan struct{}),
		refs:     1,
	}
	result := Result{SHA256: emptyDigest(), Complete: true}
	if observer.End != nil {
		observer.End(result)
	}
	store.finish(result)
	return store
}

func (c *Controller) Stream(ctx context.Context, source io.ReadCloser, expected int64, observer Observer) (*Store, error) {
	if c == nil {
		return nil, ErrStoreCapacity
	}
	if expected > c.config.MaxBodyBytes && c.config.MaxBodyBytes > 0 {
		return nil, ErrBodyTooLarge
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	store := &Store{
		controller: c,
		expected:   expected,
		observer:   observer,
		ctx:        streamCtx,
		cancel:     cancel,
		source:     source,
		notify:     make(chan struct{}),
		done:       make(chan struct{}),
		refs:       1,
	}
	if source == nil {
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
	expected := s.expected
	complete := s.complete
	size := s.size
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
		buffer := make([]byte, s.controller.config.ChunkBytes)
		n, readErr := s.source.Read(buffer)
		if n > 0 {
			data := buffer[:n:n]
			if s.expected >= 0 && total+int64(n) > s.expected {
				terminalErr = fmt.Errorf("request body exceeds declared content length: %w", io.ErrUnexpectedEOF)
				return
			}
			if s.controller.config.MaxBodyBytes > 0 && total+int64(n) > s.controller.config.MaxBodyBytes {
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
			if err := s.append(data); err != nil {
				terminalErr = err
				return
			}
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

func (s *Store) append(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleaned {
		return ErrStoreClosed
	}
	if s.discarding {
		s.size += int64(len(data))
		s.signalLocked()
		return nil
	}
	if s.file != nil {
		return s.appendFileLocked(data)
	}
	size := int64(len(data))
	withinPerBody := s.controller.config.MemoryPerBodyBytes > 0 && s.memoryBytes+size <= s.controller.config.MemoryPerBodyBytes
	if withinPerBody && s.controller.tryReserveMemory(size) {
		s.chunks = append(s.chunks, data)
		s.memoryBytes += size
		s.size += size
		s.signalLocked()
		return nil
	}
	return s.spillLocked(data)
}

func (s *Store) spillLocked(data []byte) error {
	additional := int64(len(data))
	totalDisk := s.memoryBytes + additional
	if !s.controller.tryReserveDisk(totalDisk) {
		return ErrStoreCapacity
	}
	file, path, err := s.controller.createTempFile()
	if err != nil {
		s.controller.releaseDisk(totalDisk)
		return fmt.Errorf("create request body spill file: %w", err)
	}
	offset := int64(0)
	for _, chunk := range s.chunks {
		if err := writeAtFull(file, chunk, offset); err != nil {
			_ = file.Close()
			if path != "" {
				_ = os.Remove(path)
			}
			s.controller.releaseDisk(totalDisk)
			return err
		}
		offset += int64(len(chunk))
	}
	if err := writeAtFull(file, data, offset); err != nil {
		_ = file.Close()
		if path != "" {
			_ = os.Remove(path)
		}
		s.controller.releaseDisk(totalDisk)
		return err
	}
	s.controller.releaseMemory(s.memoryBytes)
	s.chunks = nil
	s.memoryBytes = 0
	s.file = file
	s.filePath = path
	s.diskBytes = totalDisk
	s.size += additional
	s.signalLocked()
	return nil
}

func (s *Store) appendFileLocked(data []byte) error {
	size := int64(len(data))
	if !s.controller.tryReserveDisk(size) {
		return ErrStoreCapacity
	}
	if err := writeAtFull(s.file, data, s.size); err != nil {
		s.controller.releaseDisk(size)
		return err
	}
	s.diskBytes += size
	s.size += size
	s.signalLocked()
	return nil
}

func writeAtFull(file *os.File, data []byte, offset int64) error {
	for len(data) > 0 {
		n, err := file.WriteAt(data, offset)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
		offset += int64(n)
	}
	return nil
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
		s.discardStorageLocked()
		s.discarding = true
	}
	if s.refs == 0 {
		s.cleanupLocked()
	}
}

func (s *Store) discardStorageLocked() {
	if s.memoryBytes > 0 {
		s.controller.releaseMemory(s.memoryBytes)
		s.memoryBytes = 0
		s.chunks = nil
	}
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	if s.filePath != "" {
		_ = os.Remove(s.filePath)
		s.filePath = ""
	}
	if s.diskBytes > 0 {
		s.controller.releaseDisk(s.diskBytes)
		s.diskBytes = 0
	}
}

func (s *Store) cleanupLocked() {
	if s.cleaned {
		return
	}
	s.discardStorageLocked()
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
			n, err := store.readAtLocked(data, r.offset)
			r.offset += int64(n)
			store.mu.Unlock()
			return n, err
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

func (s *Store) readAtLocked(data []byte, offset int64) (int, error) {
	if s.file != nil {
		n, err := s.file.ReadAt(data, offset)
		if errors.Is(err, io.EOF) && n > 0 {
			err = nil
		}
		return n, err
	}
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
	if written == 0 && len(data) > 0 {
		return 0, io.ErrUnexpectedEOF
	}
	return written, nil
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
