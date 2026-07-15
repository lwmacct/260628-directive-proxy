package bodymemory

import (
	"bytes"
	"crypto/sha256"
	"io"
	"sync"
	"sync/atomic"
)

type Body struct {
	mu          sync.RWMutex
	data        []byte
	digest      [sha256.Size]byte
	reservation *Reservation
	refs        atomic.Int64
}

type Lease struct {
	body *Body
	once sync.Once
}

func NewBody(data []byte, reservation *Reservation) *Body {
	body := &Body{data: data, digest: sha256.Sum256(data), reservation: reservation}
	body.refs.Store(1)
	return body
}

func (b *Body) Acquire() *Lease {
	if b == nil {
		return &Lease{}
	}
	for {
		refs := b.refs.Load()
		if refs <= 0 {
			return &Lease{}
		}
		if b.refs.CompareAndSwap(refs, refs+1) {
			return &Lease{body: b}
		}
	}
}

func (b *Body) Size() int64 {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	size := len(b.data)
	b.mu.RUnlock()
	return int64(size)
}

func (b *Body) Digest() [sha256.Size]byte {
	if b == nil {
		return [sha256.Size]byte{}
	}
	return b.digest
}

func (b *Body) Release() {
	if b == nil {
		return
	}
	if refs := b.refs.Add(-1); refs != 0 {
		return
	}
	b.mu.Lock()
	b.data = nil
	reservation := b.reservation
	b.reservation = nil
	b.mu.Unlock()
	if reservation != nil {
		reservation.Close()
	}
}

func (l *Lease) Valid() bool { return l != nil && l.body != nil }

func (l *Lease) Size() int64 {
	if !l.Valid() {
		return 0
	}
	return l.body.Size()
}

func (l *Lease) Digest() [sha256.Size]byte {
	if !l.Valid() {
		return [sha256.Size]byte{}
	}
	return l.body.Digest()
}

func (l *Lease) Reader() io.ReadCloser {
	if !l.Valid() {
		return io.NopCloser(bytes.NewReader(nil))
	}
	l.body.mu.RLock()
	reader := bytes.NewReader(l.body.data)
	l.body.mu.RUnlock()
	return io.NopCloser(reader)
}

func (l *Lease) Bytes() []byte {
	if !l.Valid() {
		return nil
	}
	l.body.mu.RLock()
	data := l.body.data
	l.body.mu.RUnlock()
	return data
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.body != nil {
			l.body.Release()
			l.body = nil
		}
	})
	return nil
}
