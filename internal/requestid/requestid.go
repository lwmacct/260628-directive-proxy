package requestid

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Generator interface {
	Generate() string
}

type DefaultGenerator struct{}

func NewGenerator() *DefaultGenerator {
	return &DefaultGenerator{}
}

func (g *DefaultGenerator) Generate() string {
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

type contextKey struct{}

func ContextWith(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, requestID)
}

func FromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	requestID, ok := ctx.Value(contextKey{}).(string)
	if !ok {
		return "", false
	}
	requestID = strings.TrimSpace(requestID)
	return requestID, requestID != ""
}

func Ensure(ctx context.Context, generator Generator) (context.Context, string) {
	if requestID, ok := FromContext(ctx); ok {
		return ctx, requestID
	}
	if generator == nil {
		generator = NewGenerator()
	}
	requestID := generator.Generate()
	return ContextWith(ctx, requestID), requestID
}
