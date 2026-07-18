package program

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type scopeRuntime interface {
	emitter(producer string, attempt int) event.Emitter
	moduleFailed(moduleName string)
}

type eventSequencer interface {
	nextEventSequence() uint64
}

type mountedInstance struct {
	producer   string
	moduleName string
	instance   module.Instance
	binder     binder
	runtime    scopeRuntime
	lane       *orderedLane
	failed     atomic.Bool
}

type Scope struct {
	context   module.OpenContext
	mounted   []*mountedInstance
	closed    atomic.Bool
	attempt   atomic.Int64
	closeOnce sync.Once
}

type orderedLane struct {
	queue chan func() error
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	err   error
}

func openScope(ctx module.OpenContext, compiled []compiled, runtime scopeRuntime) (*Scope, error) {
	if len(compiled) == 0 {
		return nil, nil
	}
	scope := &Scope{context: ctx}
	scope.attempt.Store(int64(ctx.Attempt))
	for _, item := range compiled {
		instance, err := item.binding.Open(ctx)
		if err != nil {
			_ = scope.Finish(context.Background(), module.FinishFailed)
			return nil, fmt.Errorf("open module %q (%s): %w", item.moduleName, item.id, err)
		}
		if instance == nil {
			_ = scope.Finish(context.Background(), module.FinishFailed)
			return nil, fmt.Errorf("open module %q (%s): nil instance", item.moduleName, item.id)
		}
		mounted := &mountedInstance{producer: item.id, moduleName: item.moduleName, instance: instance}
		mounted.runtime = runtime
		scope.mounted = append(scope.mounted, mounted)
		if err := mounted.call(func() error {
			instance.Bind(&mounted.binder)
			return nil
		}); err != nil {
			_ = scope.Finish(context.Background(), module.FinishFailed)
			return nil, fmt.Errorf("bind module %q (%s): %w", item.moduleName, item.id, err)
		}
		if mounted.binder.needsLane() {
			mounted.lane = newOrderedLane(mounted.binder.laneCapacity())
		}
	}
	return scope, nil
}

func (s *Scope) SetAttempt(attempt int) {
	if s != nil {
		s.attempt.Store(int64(attempt))
	}
}

func (s *Scope) HasOutboundBodyMutators() bool {
	if s == nil {
		return false
	}
	for _, mounted := range s.mounted {
		if len(mounted.binder.outboundBodyChunk) > 0 {
			return true
		}
	}
	return false
}

func (s *Scope) HasUpstreamBodyMutators() bool {
	if s == nil {
		return false
	}
	for _, mounted := range s.mounted {
		if len(mounted.binder.upstreamBodyDraft) > 0 {
			return true
		}
	}
	return false
}

func (s *Scope) Finish(ctx context.Context, cause module.FinishCause) error {
	if s == nil {
		return nil
	}
	var result error
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		for _, mounted := range s.mounted {
			if mounted.lane != nil {
				result = errors.Join(result, mounted.lane.close(ctx))
			}
			finishCtx := module.FinishContext{Context: s.eventContext(ctx, mounted), Cause: cause}
			result = errors.Join(result, mounted.finish(finishCtx))
		}
	})
	return result
}

func (s *Scope) RequestStarted(ctx context.Context, value lifecycle.RequestStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.RequestStarted] { return b.requestStarted }, cloneRequestStarted)
}

func (s *Scope) RequestBodyChunk(ctx context.Context, value lifecycle.BodyChunk) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.BodyChunk] { return b.requestBodyChunk }, cloneBodyChunk)
}

func (s *Scope) RequestBodyEnded(ctx context.Context, value lifecycle.RequestBodyEnded) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.RequestBodyEnded] { return b.requestBodyEnded }, nil)
}

func (s *Scope) AttemptStarted(ctx context.Context, value lifecycle.AttemptStarted) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.AttemptStarted] { return b.attemptStarted }, nil)
}

func (s *Scope) DirectiveResolved(ctx context.Context, value lifecycle.DirectiveResolved) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.DirectiveResolved] { return b.directiveResolved }, cloneDirectiveResolved)
}

func (s *Scope) DirectiveFailed(ctx context.Context, value lifecycle.DirectiveFailed) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.DirectiveFailed] { return b.directiveFailed }, nil)
}

func (s *Scope) MetadataBound(ctx context.Context, value lifecycle.MetadataBound) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.MetadataBound] { return b.metadataBound }, cloneMetadataBound)
}

func (s *Scope) MetadataChanged(ctx context.Context, value lifecycle.MetadataChanged) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.MetadataChanged] { return b.metadataChanged }, cloneMetadataChanged)
}

func (s *Scope) UpstreamStarted(ctx context.Context, value lifecycle.UpstreamStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.UpstreamStarted] { return b.upstreamStarted }, cloneUpstreamStarted)
}

func (s *Scope) UpstreamResponseStarted(ctx context.Context, value lifecycle.ResponseStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.ResponseStarted] { return b.upstreamResponse }, cloneResponseStarted)
}

func (s *Scope) UpstreamJSONChunk(ctx context.Context, value lifecycle.BodyChunk) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.BodyChunk] { return b.upstreamJSONChunk }, cloneBodyChunk)
}

func (s *Scope) UpstreamBodyChunk(ctx context.Context, value lifecycle.BodyChunk) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.BodyChunk] { return b.upstreamBodyChunk }, cloneBodyChunk)
}

func (s *Scope) UpstreamSSEData(ctx context.Context, value lifecycle.SSEData) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.SSEData] { return b.upstreamSSEData }, cloneSSEData)
}

func (s *Scope) UpstreamBodyEnded(ctx context.Context, value lifecycle.BodyEnded) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.BodyEnded] { return b.upstreamBodyEnded }, nil)
}

func (s *Scope) AttemptFinished(ctx context.Context, value lifecycle.AttemptFinished) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.AttemptFinished] { return b.attemptFinished }, nil)
}

func (s *Scope) RecoveryStarted(ctx context.Context, value lifecycle.RecoveryStarted) error {
	value = cloneRecoveryStarted(value)
	return dispatchWithID(s, ctx, value.EventID, value, func(b *binder) []subscription[lifecycle.RecoveryStarted] { return b.recoveryStarted }, cloneRecoveryStarted)
}

func (s *Scope) RecoveryDecided(ctx context.Context, value lifecycle.RecoveryDecided) error {
	return dispatchWithID(s, ctx, value.EventID, value, func(b *binder) []subscription[lifecycle.RecoveryDecided] { return b.recoveryDecided }, nil)
}

func (s *Scope) RecoveryFinished(ctx context.Context, value lifecycle.RecoveryFinished) error {
	return dispatchWithID(s, ctx, value.EventID, value, func(b *binder) []subscription[lifecycle.RecoveryFinished] { return b.recoveryFinished }, nil)
}

func (s *Scope) DownstreamResponseStarted(ctx context.Context, value lifecycle.ResponseStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.ResponseStarted] { return b.downstreamResponse }, cloneResponseStarted)
}

func (s *Scope) DownstreamBodyChunk(ctx context.Context, value lifecycle.BodyChunk) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.BodyChunk] { return b.downstreamBodyChunk }, cloneBodyChunk)
}

func (s *Scope) DownstreamSSEData(ctx context.Context, value lifecycle.SSEData) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.SSEData] { return b.downstreamSSEData }, cloneSSEData)
}

func (s *Scope) DownstreamSSEComment(ctx context.Context, value lifecycle.SSEComment) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.SSEComment] { return b.downstreamSSEComment }, nil)
}

func (s *Scope) DownstreamBodyEnded(ctx context.Context, value lifecycle.BodyEnded) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.BodyEnded] { return b.downstreamBodyEnded }, nil)
}

func (s *Scope) RequestFinished(ctx context.Context, value lifecycle.RequestFinished) error {
	return dispatch(s, ctx, value, func(b *binder) []subscription[lifecycle.RequestFinished] { return b.requestFinished }, nil)
}

func (s *Scope) MutateOutboundRequest(ctx context.Context, request *http.Request) error {
	if request == nil {
		return nil
	}
	return mutate(s, ctx, request, func(b *binder) []mutation[http.Request] { return b.outboundRequest })
}

func (s *Scope) MutateOutboundBodyChunk(ctx context.Context, draft *lifecycle.BodyDraft) error {
	return mutate(s, ctx, draft, func(b *binder) []mutation[lifecycle.BodyDraft] { return b.outboundBodyChunk })
}

func (s *Scope) MutateUpstreamResponse(ctx context.Context, draft *lifecycle.ResponseDraft) error {
	return mutate(s, ctx, draft, func(b *binder) []mutation[lifecycle.ResponseDraft] { return b.upstreamDraft })
}

func (s *Scope) MutateUpstreamBodyChunk(ctx context.Context, draft *lifecycle.BodyDraft) error {
	return mutate(s, ctx, draft, func(b *binder) []mutation[lifecycle.BodyDraft] { return b.upstreamBodyDraft })
}

func dispatch[T any](s *Scope, ctx context.Context, value T, selectHandlers func(*binder) []subscription[T], clone func(T) T) error {
	return dispatchAtWithID(s, ctx, time.Time{}, "", value, selectHandlers, clone)
}

func dispatchAt[T any](s *Scope, ctx context.Context, observedAt time.Time, value T, selectHandlers func(*binder) []subscription[T], clone func(T) T) error {
	return dispatchAtWithID(s, ctx, observedAt, "", value, selectHandlers, clone)
}

func dispatchWithID[T any](s *Scope, ctx context.Context, eventID string, value T, selectHandlers func(*binder) []subscription[T], clone func(T) T) error {
	return dispatchAtWithID(s, ctx, time.Time{}, eventID, value, selectHandlers, clone)
}

func dispatchAtWithID[T any](s *Scope, ctx context.Context, observedAt time.Time, eventID string, value T, selectHandlers func(*binder) []subscription[T], clone func(T) T) error {
	if s == nil || s.closed.Load() {
		return nil
	}
	sequence := s.nextEventSequence()
	var result error
	for _, mounted := range s.mounted {
		for _, item := range selectHandlers(&mounted.binder) {
			current := value
			if item.policy.Executor == module.ExecutorOrderedLane && clone != nil {
				current = clone(value)
			}
			eventCtx := s.eventContextAt(ctx, observedAt, eventID, sequence, mounted)
			task := func() error { return mounted.call(func() error { return item.handle(eventCtx, current) }) }
			if item.policy.Executor == module.ExecutorCaller {
				result = errors.Join(result, task())
				continue
			}
			if item.policy.Barrier != module.BarrierBeforeCommit {
				if _, err := mounted.lane.submit(ctx, item.policy, task); err != nil {
					result = errors.Join(result, err)
				}
				continue
			}
			completed := make(chan error, 1)
			accepted, err := mounted.lane.submit(ctx, item.policy, func() error {
				err := task()
				completed <- err
				return err
			})
			if err != nil {
				result = errors.Join(result, err)
				continue
			}
			if !accepted {
				continue
			}
			select {
			case err := <-completed:
				result = errors.Join(result, err)
			case <-ctx.Done():
				result = errors.Join(result, ctx.Err())
			}
		}
	}
	return result
}

func mutate[T any](s *Scope, ctx context.Context, value *T, selectHandlers func(*binder) []mutation[T]) error {
	if s == nil || value == nil || s.closed.Load() {
		return nil
	}
	for _, mounted := range s.mounted {
		for _, item := range selectHandlers(&mounted.binder) {
			eventCtx := s.eventContext(ctx, mounted)
			task := func() error { return mounted.call(func() error { return item.handle(eventCtx, value) }) }
			if item.policy.Executor == module.ExecutorCaller {
				if err := task(); err != nil {
					return err
				}
				continue
			}
			result := make(chan error, 1)
			_, err := mounted.lane.submit(ctx, module.Policy{Executor: module.ExecutorOrderedLane, Barrier: module.BarrierBeforeCommit, Overflow: module.OverflowBlock}, func() error {
				err := task()
				result <- err
				return err
			})
			if err != nil {
				return err
			}
			select {
			case err := <-result:
				if err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (s *Scope) eventContext(ctx context.Context, mounted *mountedInstance) module.Context {
	return s.eventContextAt(ctx, time.Time{}, "", s.nextEventSequence(), mounted)
}

func (s *Scope) eventContextAt(ctx context.Context, observedAt time.Time, eventID string, sequence uint64, mounted *mountedInstance) module.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	attempt := int(s.attempt.Load())
	var emitter event.Emitter
	if mounted.runtime != nil {
		emitter = mounted.runtime.emitter(mounted.producer, attempt)
	}
	if observedAt.IsZero() {
		observedAt = nowUTC()
	}
	return module.Context{Context: ctx, TraceID: s.context.TraceID, Attempt: attempt, EventID: eventID, Sequence: sequence, ObservedAt: observedAt, Emitter: emitter}
}

func (s *Scope) nextEventSequence() uint64 {
	if s == nil {
		return 0
	}
	for _, mounted := range s.mounted {
		if sequencer, ok := mounted.runtime.(eventSequencer); ok {
			return sequencer.nextEventSequence()
		}
	}
	return 0
}

func (m *mountedInstance) call(run func() error) (err error) {
	if m == nil || m.failed.Load() {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			m.failed.Store(true)
			if m.runtime != nil {
				m.runtime.moduleFailed(m.moduleName)
			}
			err = fmt.Errorf("module %q panicked: %v", m.producer, recovered)
		}
	}()
	err = run()
	if err != nil {
		m.failed.Store(true)
		if m.runtime != nil {
			m.runtime.moduleFailed(m.moduleName)
		}
	}
	return err
}

func (m *mountedInstance) finish(ctx module.FinishContext) (err error) {
	if m == nil || m.instance == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if m.runtime != nil {
				m.runtime.moduleFailed(m.moduleName)
			}
			err = fmt.Errorf("finish module %q panicked: %v", m.producer, recovered)
			return
		}
		if err != nil && m.runtime != nil {
			m.runtime.moduleFailed(m.moduleName)
		}
	}()
	return m.instance.Finish(ctx)
}

func newOrderedLane(capacity int) *orderedLane {
	if capacity <= 0 {
		capacity = 128
	}
	lane := &orderedLane{queue: make(chan func() error, capacity), done: make(chan struct{})}
	go lane.run()
	return lane
}

func (l *orderedLane) submit(ctx context.Context, policy module.Policy, task func() error) (bool, error) {
	if l == nil || task == nil {
		return false, nil
	}
	switch policy.Overflow {
	case module.OverflowDrop:
		select {
		case l.queue <- task:
			return true, nil
		default:
			return false, nil
		}
	case module.OverflowFailRequest:
		select {
		case l.queue <- task:
			return true, nil
		default:
			return false, errors.New("module ordered lane is full")
		}
	default:
		select {
		case l.queue <- task:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func (l *orderedLane) run() {
	defer close(l.done)
	for task := range l.queue {
		if err := task(); err != nil {
			l.mu.Lock()
			l.err = errors.Join(l.err, err)
			l.mu.Unlock()
		}
	}
}

func (l *orderedLane) close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { close(l.queue) })
	select {
	case <-l.done:
		l.mu.Lock()
		err := l.err
		l.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *binder) needsLane() bool {
	return b.laneCapacity() > 0
}

func (b *binder) laneCapacity() int {
	capacity := 0
	accept := func(policy module.Policy) {
		if policy.Executor == module.ExecutorOrderedLane && policy.Capacity > capacity {
			capacity = policy.Capacity
		}
	}
	for _, item := range b.allPolicies() {
		accept(item)
	}
	return capacity
}

func (b *binder) allPolicies() []module.Policy {
	var result []module.Policy
	appendSubscriptions := func(values ...[]module.Policy) {
		for _, value := range values {
			result = append(result, value...)
		}
	}
	policies := func(length int, at func(int) module.Policy) []module.Policy {
		items := make([]module.Policy, length)
		for index := range items {
			items[index] = at(index)
		}
		return items
	}
	appendSubscriptions(
		policies(len(b.requestStarted), func(i int) module.Policy { return b.requestStarted[i].policy }),
		policies(len(b.requestBodyChunk), func(i int) module.Policy { return b.requestBodyChunk[i].policy }),
		policies(len(b.requestBodyEnded), func(i int) module.Policy { return b.requestBodyEnded[i].policy }),
		policies(len(b.attemptStarted), func(i int) module.Policy { return b.attemptStarted[i].policy }),
		policies(len(b.directiveResolved), func(i int) module.Policy { return b.directiveResolved[i].policy }),
		policies(len(b.directiveFailed), func(i int) module.Policy { return b.directiveFailed[i].policy }),
		policies(len(b.metadataBound), func(i int) module.Policy { return b.metadataBound[i].policy }),
		policies(len(b.metadataChanged), func(i int) module.Policy { return b.metadataChanged[i].policy }),
		policies(len(b.upstreamStarted), func(i int) module.Policy { return b.upstreamStarted[i].policy }),
		policies(len(b.upstreamResponse), func(i int) module.Policy { return b.upstreamResponse[i].policy }),
		policies(len(b.upstreamBodyChunk), func(i int) module.Policy { return b.upstreamBodyChunk[i].policy }),
		policies(len(b.upstreamJSONChunk), func(i int) module.Policy { return b.upstreamJSONChunk[i].policy }),
		policies(len(b.upstreamSSEData), func(i int) module.Policy { return b.upstreamSSEData[i].policy }),
		policies(len(b.upstreamBodyEnded), func(i int) module.Policy { return b.upstreamBodyEnded[i].policy }),
		policies(len(b.attemptFinished), func(i int) module.Policy { return b.attemptFinished[i].policy }),
		policies(len(b.recoveryStarted), func(i int) module.Policy { return b.recoveryStarted[i].policy }),
		policies(len(b.recoveryDecided), func(i int) module.Policy { return b.recoveryDecided[i].policy }),
		policies(len(b.recoveryFinished), func(i int) module.Policy { return b.recoveryFinished[i].policy }),
		policies(len(b.downstreamResponse), func(i int) module.Policy { return b.downstreamResponse[i].policy }),
		policies(len(b.downstreamBodyChunk), func(i int) module.Policy { return b.downstreamBodyChunk[i].policy }),
		policies(len(b.downstreamSSEData), func(i int) module.Policy { return b.downstreamSSEData[i].policy }),
		policies(len(b.downstreamSSEComment), func(i int) module.Policy { return b.downstreamSSEComment[i].policy }),
		policies(len(b.downstreamBodyEnded), func(i int) module.Policy { return b.downstreamBodyEnded[i].policy }),
		policies(len(b.requestFinished), func(i int) module.Policy { return b.requestFinished[i].policy }),
		policies(len(b.outboundRequest), func(i int) module.Policy { return b.outboundRequest[i].policy }),
		policies(len(b.outboundBodyChunk), func(i int) module.Policy { return b.outboundBodyChunk[i].policy }),
		policies(len(b.upstreamDraft), func(i int) module.Policy { return b.upstreamDraft[i].policy }),
		policies(len(b.upstreamBodyDraft), func(i int) module.Policy { return b.upstreamBodyDraft[i].policy }),
	)
	return result
}

func cloneRequestStarted(value lifecycle.RequestStarted) lifecycle.RequestStarted {
	value.Header = value.Header.Clone()
	return value
}

func cloneDirectiveResolved(value lifecycle.DirectiveResolved) lifecycle.DirectiveResolved {
	if value.Target != nil {
		target := *value.Target
		value.Target = &target
	}
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneMetadataBound(value lifecycle.MetadataBound) lifecycle.MetadataBound {
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneMetadataChanged(value lifecycle.MetadataChanged) lifecycle.MetadataChanged {
	value.Bound = cloneMetadata(value.Bound)
	value.Observed = cloneMetadata(value.Observed)
	return value
}

func cloneUpstreamStarted(value lifecycle.UpstreamStarted) lifecycle.UpstreamStarted {
	value.Header = value.Header.Clone()
	return value
}

func cloneResponseStarted(value lifecycle.ResponseStarted) lifecycle.ResponseStarted {
	value.Header = value.Header.Clone()
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneBodyChunk(value lifecycle.BodyChunk) lifecycle.BodyChunk {
	value.Data = append([]byte(nil), value.Data...)
	return value
}

func cloneSSEData(value lifecycle.SSEData) lifecycle.SSEData {
	value.Data = append([]byte(nil), value.Data...)
	if value.RetryMillis != nil {
		retry := *value.RetryMillis
		value.RetryMillis = &retry
	}
	return value
}

func cloneRecoveryStarted(value lifecycle.RecoveryStarted) lifecycle.RecoveryStarted {
	value.Metadata = cloneMetadata(value.Metadata)
	value.ControllerHeaders = value.ControllerHeaders.Clone()
	if value.Response != nil {
		response := *value.Response
		response.Header = value.Response.Header.Clone()
		if value.Response.Body != nil {
			body := *value.Response.Body
			response.Body = &body
		}
		value.Response = &response
	}
	return value
}

func cloneMetadata(value map[string][]string) map[string][]string {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(value))
	for name, values := range value {
		cloned[name] = append([]string(nil), values...)
	}
	return cloned
}

func nowUTC() time.Time { return time.Now().UTC() }
