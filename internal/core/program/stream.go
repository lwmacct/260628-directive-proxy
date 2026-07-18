package program

import (
	"context"
	"errors"
	"mime"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/sse"
)

type StreamObserver interface {
	Observe(context.Context, time.Time, []byte) error
	Finish(context.Context, time.Time) error
}

type streamDirection uint8

const (
	streamUpstream streamDirection = iota
	streamDownstream
)

type streamObserver struct {
	scopes         *ScopeSet
	direction      streamDirection
	contentType    string
	sse            *sse.Parser
	jsonSubscribed bool
	commentIndex   uint64
	ctx            context.Context
	observedAt     time.Time
	err            error
}

func NewUpstreamObserver(contentType string, maxSSEEventBytes int, scopes *ScopeSet) StreamObserver {
	return newStreamObserver(streamUpstream, contentType, maxSSEEventBytes, scopes)
}

func NewDownstreamObserver(contentType string, maxSSEEventBytes int, scopes *ScopeSet) StreamObserver {
	return newStreamObserver(streamDownstream, contentType, maxSSEEventBytes, scopes)
}

func newStreamObserver(direction streamDirection, contentType string, maxSSEEventBytes int, scopes *ScopeSet) *streamObserver {
	observer := &streamObserver{direction: direction, contentType: contentType, scopes: scopes}
	sseSubscribed := false
	commentsSubscribed := false
	if observer.scopes != nil {
		for _, entry := range observer.scopes.mounted {
			if entry.scope.closed.Load() {
				continue
			}
			if direction == streamUpstream {
				sseSubscribed = sseSubscribed || len(entry.mounted.binder.upstreamSSEData) > 0
				observer.jsonSubscribed = observer.jsonSubscribed || len(entry.mounted.binder.upstreamJSONChunk) > 0
			} else {
				sseSubscribed = sseSubscribed || len(entry.mounted.binder.downstreamSSEData) > 0
				commentsSubscribed = commentsSubscribed || len(entry.mounted.binder.downstreamSSEComment) > 0
			}
		}
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if (sseSubscribed || commentsSubscribed) && strings.EqualFold(mediaType, "text/event-stream") {
		observer.sse = sse.NewParser(maxSSEEventBytes, observer.onSSEEvent, observer.onSSEComment)
	}
	return observer
}

func (observer *streamObserver) Observe(ctx context.Context, observedAt time.Time, data []byte) error {
	if observer == nil || len(data) == 0 {
		return nil
	}
	observer.ctx = ctx
	observer.observedAt = observedAt
	if observer.sse != nil {
		observer.sse.Feed(data)
		return observer.err
	}
	if observer.direction == streamUpstream && observer.jsonSubscribed && isJSONContentType(observer.contentType) {
		if err := observer.scopes.upstreamJSONChunkAt(ctx, observedAt, lifecycle.BodyChunk{Data: data}); err != nil {
			return err
		}
	}
	return nil
}

func (observer *streamObserver) Finish(ctx context.Context, observedAt time.Time) error {
	if observer != nil && observer.sse != nil {
		observer.ctx = ctx
		observer.observedAt = observedAt
		observer.sse.Close()
	}
	if observer == nil {
		return nil
	}
	return observer.err
}

func (observer *streamObserver) onSSEEvent(event sse.Event) {
	if observer == nil {
		return
	}
	value := lifecycle.SSEData{
		Sequence: event.Sequence, Event: event.Type, ID: event.ID, Data: []byte(event.Data),
		RetryMillis: event.RetryMillis, Truncated: event.Truncated,
	}
	if observer.direction == streamUpstream {
		observer.err = errors.Join(observer.err, observer.scopes.upstreamSSEDataAt(observer.ctx, observer.observedAt, value))
	} else {
		observer.err = errors.Join(observer.err, observer.scopes.downstreamSSEDataAt(observer.ctx, observer.observedAt, value))
	}
}

func (observer *streamObserver) onSSEComment(comment string) {
	if observer == nil || observer.direction != streamDownstream {
		return
	}
	observer.commentIndex++
	observer.err = errors.Join(observer.err, observer.scopes.downstreamSSECommentAt(observer.ctx, observer.observedAt, lifecycle.SSEComment{
		Sequence: observer.commentIndex,
		Comment:  comment,
	}))
}

func (s *ScopeSet) upstreamJSONChunkAt(ctx context.Context, observedAt time.Time, value lifecycle.BodyChunk) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *binder) []subscription[lifecycle.BodyChunk] { return b.upstreamJSONChunk }, cloneBodyChunk)
}

func (s *ScopeSet) upstreamSSEDataAt(ctx context.Context, observedAt time.Time, value lifecycle.SSEData) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *binder) []subscription[lifecycle.SSEData] { return b.upstreamSSEData }, cloneSSEData)
}

func (s *ScopeSet) downstreamSSEDataAt(ctx context.Context, observedAt time.Time, value lifecycle.SSEData) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *binder) []subscription[lifecycle.SSEData] { return b.downstreamSSEData }, cloneSSEData)
}

func (s *ScopeSet) downstreamSSECommentAt(ctx context.Context, observedAt time.Time, value lifecycle.SSEComment) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *binder) []subscription[lifecycle.SSEComment] { return b.downstreamSSEComment }, nil)
}

func isJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")
}
