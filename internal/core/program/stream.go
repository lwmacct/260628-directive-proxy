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

type streamObserver struct {
	scopes       *ScopeSet
	sse          *sse.Parser
	commentIndex uint64
	ctx          context.Context
	observedAt   time.Time
	err          error
}

func NewDownstreamObserver(contentType string, maxSSEEventBytes int, scopes *ScopeSet) StreamObserver {
	observer := &streamObserver{scopes: scopes}
	sseSubscribed := false
	commentsSubscribed := false
	if observer.scopes != nil {
		for _, entry := range observer.scopes.mounted {
			if entry.scope.closed.Load() {
				continue
			}
			sseSubscribed = sseSubscribed || len(entry.mounted.binder.downstreamSSEData) > 0
			commentsSubscribed = commentsSubscribed || len(entry.mounted.binder.downstreamSSEComment) > 0
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
	observer.err = errors.Join(observer.err, observer.scopes.downstreamSSEDataAt(observer.ctx, observer.observedAt, value))
}

func (observer *streamObserver) onSSEComment(comment string) {
	if observer == nil {
		return
	}
	observer.commentIndex++
	observer.err = errors.Join(observer.err, observer.scopes.downstreamSSECommentAt(observer.ctx, observer.observedAt, lifecycle.SSEComment{
		Sequence: observer.commentIndex,
		Comment:  comment,
	}))
}

func (s *ScopeSet) downstreamSSEDataAt(ctx context.Context, observedAt time.Time, value lifecycle.SSEData) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *binder) []subscription[lifecycle.SSEData] { return b.downstreamSSEData }, cloneSSEData)
}

func (s *ScopeSet) downstreamSSECommentAt(ctx context.Context, observedAt time.Time, value lifecycle.SSEComment) error {
	return dispatchAt(s, ctx, observedAt, value, func(b *binder) []subscription[lifecycle.SSEComment] { return b.downstreamSSEComment }, nil)
}
