package event

import "github.com/lwmacct/260628-directive-proxy/internal/core/metadata"

type Emitter interface {
	Emit(topic string, data map[string]any) bool
	EmitOwned(topic string, data map[string]any, release func()) bool
	EmitBorrowed(topic string, data map[string]any) bool
}

type Session interface {
	Emitter(producer string, roundTrip int) Emitter
	Close()
}

type Provider interface {
	Open(traceID string, fields metadata.Set) Session
}
