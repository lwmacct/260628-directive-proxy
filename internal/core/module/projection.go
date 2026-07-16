package module

import (
	"context"
	"errors"
	"mime"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/sse"
)

type ProjectionDirection uint8

const (
	ProjectionUpstream ProjectionDirection = iota
	ProjectionDownstream
)

type Projection struct {
	scopes         []*Scope
	direction      ProjectionDirection
	contentType    string
	sse            *sse.Parser
	jsonSubscribed bool
	commentIndex   uint64
	ctx            context.Context
	observedAt     time.Time
	err            error
}

func NewProjection(direction ProjectionDirection, contentType string, maxSSEEventBytes int, scopes ...*Scope) *Projection {
	projection := &Projection{direction: direction, contentType: contentType}
	for _, scope := range scopes {
		if scope != nil {
			projection.scopes = append(projection.scopes, scope)
		}
	}
	sseSubscribed := false
	commentsSubscribed := false
	for _, scope := range projection.scopes {
		for _, mounted := range scope.mounted {
			if direction == ProjectionUpstream {
				sseSubscribed = sseSubscribed || len(mounted.binder.upstreamSSEData) > 0
				projection.jsonSubscribed = projection.jsonSubscribed || len(mounted.binder.upstreamJSONChunk) > 0
			} else {
				sseSubscribed = sseSubscribed || len(mounted.binder.downstreamSSEData) > 0
				commentsSubscribed = commentsSubscribed || len(mounted.binder.downstreamSSEComment) > 0
			}
		}
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if (sseSubscribed || commentsSubscribed) && strings.EqualFold(mediaType, "text/event-stream") {
		projection.sse = sse.NewParser(maxSSEEventBytes, projection.onSSEEvent, projection.onSSEComment)
	}
	return projection
}

func (p *Projection) Feed(ctx context.Context, observedAt time.Time, data []byte) error {
	if p == nil || len(data) == 0 {
		return nil
	}
	p.ctx = ctx
	p.observedAt = observedAt
	if p.sse != nil {
		p.sse.Feed(data)
		return p.err
	}
	if p.direction == ProjectionUpstream && p.jsonSubscribed && isJSONContentType(p.contentType) {
		for _, scope := range p.scopes {
			if err := scope.upstreamJSONChunkAt(ctx, observedAt, BodyChunk{Data: data}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Projection) Close(ctx context.Context, observedAt time.Time) error {
	if p != nil && p.sse != nil {
		p.ctx = ctx
		p.observedAt = observedAt
		p.sse.Close()
	}
	if p == nil {
		return nil
	}
	return p.err
}

func (p *Projection) onSSEEvent(event sse.Event) {
	if p == nil {
		return
	}
	value := SSEData{
		Sequence: event.Sequence, Event: event.Type, ID: event.ID, Data: []byte(event.Data),
		RetryMillis: event.RetryMillis, Truncated: event.Truncated,
	}
	for _, scope := range p.scopes {
		if p.direction == ProjectionUpstream {
			p.err = errors.Join(p.err, scope.upstreamSSEDataAt(p.ctx, p.observedAt, value))
		} else {
			p.err = errors.Join(p.err, scope.downstreamSSEDataAt(p.ctx, p.observedAt, value))
		}
	}
}

func (p *Projection) onSSEComment(comment string) {
	if p == nil || p.direction != ProjectionDownstream {
		return
	}
	p.commentIndex++
	for _, scope := range p.scopes {
		p.err = errors.Join(p.err, scope.downstreamSSECommentAt(p.ctx, p.observedAt, SSEComment{Sequence: p.commentIndex, Comment: comment}))
	}
}

func (s *Scope) upstreamJSONChunkAt(ctx context.Context, observedAt time.Time, value BodyChunk) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *Binder) []subscription[BodyChunk] { return b.upstreamJSONChunk }, cloneBodyChunk)
}

func (s *Scope) upstreamSSEDataAt(ctx context.Context, observedAt time.Time, value SSEData) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *Binder) []subscription[SSEData] { return b.upstreamSSEData }, cloneSSEData)
}

func (s *Scope) downstreamSSEDataAt(ctx context.Context, observedAt time.Time, value SSEData) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *Binder) []subscription[SSEData] { return b.downstreamSSEData }, cloneSSEData)
}

func (s *Scope) downstreamSSECommentAt(ctx context.Context, observedAt time.Time, value SSEComment) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *Binder) []subscription[SSEComment] { return b.downstreamSSEComment }, nil)
}

func isJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")
}
