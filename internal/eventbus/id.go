package eventbus

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"strings"
	"time"

	"github.com/google/uuid"
)

type IDGenerator interface {
	Generate() string
}

type DefaultIDGenerator struct{}

func NewIDGenerator() *DefaultIDGenerator {
	return &DefaultIDGenerator{}
}

func (g *DefaultIDGenerator) Generate() string {
	id, err := uuid.NewV7()
	if err != nil {
		return generateFallbackID()
	}
	return id.String()
}

func generateFallbackID() string {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[:8], uint64(time.Now().UnixMilli()))
	if _, err := rand.Read(raw[8:]); err != nil {
		binary.BigEndian.PutUint64(raw[8:], uint64(time.Now().UnixNano()))
	}

	raw[6] = (raw[6] & 0x0f) | 0x70
	raw[8] = (raw[8] & 0x3f) | 0x80

	return uuid.UUID(raw).String()
}

type requestIDContextKey struct{}

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	requestID, ok := ctx.Value(requestIDContextKey{}).(string)
	if !ok {
		return "", false
	}
	requestID = strings.TrimSpace(requestID)
	return requestID, requestID != ""
}

func EnsureRequestID(ctx context.Context, generator IDGenerator) (context.Context, string) {
	if requestID, ok := RequestIDFromContext(ctx); ok {
		return ctx, requestID
	}
	if generator == nil {
		generator = NewIDGenerator()
	}
	requestID := generator.Generate()
	return ContextWithRequestID(ctx, requestID), requestID
}
