package module

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type OutputFactory interface {
	Output(producer string, attempt int) Output
	ModuleFailed(producer string)
}

type mountedInstance struct {
	producer   string
	moduleName string
	instance   Instance
	binder     Binder
	outputs    OutputFactory
	lane       *orderedLane
	failed     atomic.Bool
}

type Scope struct {
	context   OpenContext
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

func OpenScope(ctx OpenContext, compiled []Compiled, outputs OutputFactory) (*Scope, error) {
	scope := &Scope{context: ctx}
	scope.attempt.Store(int64(ctx.Attempt))
	for _, item := range compiled {
		instance, err := item.Binding.Open(ctx)
		if err != nil {
			_ = scope.Finish(context.Background(), FinishFailed)
			return nil, fmt.Errorf("open module %q (%s): %w", item.Spec.Module, item.Spec.ID, err)
		}
		if instance == nil {
			_ = scope.Finish(context.Background(), FinishFailed)
			return nil, fmt.Errorf("open module %q (%s): nil instance", item.Spec.Module, item.Spec.ID)
		}
		mounted := &mountedInstance{producer: item.Spec.ID, moduleName: item.Spec.Module, instance: instance}
		mounted.outputs = outputs
		instance.Mount(&mounted.binder)
		if mounted.binder.needsLane() {
			mounted.lane = newOrderedLane(mounted.binder.laneCapacity())
		}
		scope.mounted = append(scope.mounted, mounted)
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
		if len(mounted.binder.outboundBody) > 0 {
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

func (s *Scope) Finish(ctx context.Context, cause FinishCause) error {
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
			finishCtx := FinishContext{Context: ctx, EventContext: s.eventContext(ctx, mounted), Cause: cause}
			result = errors.Join(result, mounted.call(func() error { return mounted.instance.Finish(finishCtx) }))
		}
	})
	return result
}

func (s *Scope) RequestStarted(ctx context.Context, value RequestStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *Binder) []subscription[RequestStarted] { return b.requestStarted }, cloneRequestStarted)
}

func (s *Scope) RequestBodyAvailable(ctx context.Context, value RequestBodyAvailable) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[RequestBodyAvailable] { return b.requestBodyAvailable }, nil)
}

func (s *Scope) RequestBodyEnded(ctx context.Context, value RequestBodyEnded) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[RequestBodyEnded] { return b.requestBodyEnded }, nil)
}

func (s *Scope) AttemptStarted(ctx context.Context, value AttemptStarted) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[AttemptStarted] { return b.attemptStarted }, nil)
}

func (s *Scope) DirectiveResolved(ctx context.Context, value DirectiveResolved) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[DirectiveResolved] { return b.directiveResolved }, cloneDirectiveResolved)
}

func (s *Scope) DirectiveFailed(ctx context.Context, value DirectiveFailed) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[DirectiveFailed] { return b.directiveFailed }, nil)
}

func (s *Scope) MetadataBound(ctx context.Context, value MetadataBound) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[MetadataBound] { return b.metadataBound }, cloneMetadataBound)
}

func (s *Scope) MetadataChanged(ctx context.Context, value MetadataChanged) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[MetadataChanged] { return b.metadataChanged }, cloneMetadataChanged)
}

func (s *Scope) UpstreamStarted(ctx context.Context, value UpstreamStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *Binder) []subscription[UpstreamStarted] { return b.upstreamStarted }, cloneUpstreamStarted)
}

func (s *Scope) UpstreamResponseStarted(ctx context.Context, value ResponseStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *Binder) []subscription[ResponseStarted] { return b.upstreamResponse }, cloneResponseStarted)
}

func (s *Scope) UpstreamJSONChunk(ctx context.Context, value BodyChunk) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[BodyChunk] { return b.upstreamJSONChunk }, cloneBodyChunk)
}

func (s *Scope) UpstreamBodyChunk(ctx context.Context, value BodyChunk) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[BodyChunk] { return b.upstreamBodyChunk }, cloneBodyChunk)
}

func (s *Scope) UpstreamSSEData(ctx context.Context, value SSEData) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[SSEData] { return b.upstreamSSEData }, cloneSSEData)
}

func (s *Scope) UpstreamBodyEnded(ctx context.Context, value BodyEnded) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[BodyEnded] { return b.upstreamBodyEnded }, nil)
}

func (s *Scope) AttemptFinished(ctx context.Context, value AttemptFinished) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[AttemptFinished] { return b.attemptFinished }, nil)
}

func (s *Scope) RetryRequested(ctx context.Context, value RetryRequested) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[RetryRequested] { return b.retryRequested }, cloneRetryRequested)
}

func (s *Scope) DownstreamResponseStarted(ctx context.Context, value ResponseStarted) error {
	value.Header = value.Header.Clone()
	return dispatch(s, ctx, value, func(b *Binder) []subscription[ResponseStarted] { return b.downstreamResponse }, cloneResponseStarted)
}

func (s *Scope) DownstreamBodyChunk(ctx context.Context, value BodyChunk) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[BodyChunk] { return b.downstreamBodyChunk }, cloneBodyChunk)
}

func (s *Scope) DownstreamSSEData(ctx context.Context, value SSEData) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[SSEData] { return b.downstreamSSEData }, cloneSSEData)
}

func (s *Scope) DownstreamSSEComment(ctx context.Context, value SSEComment) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[SSEComment] { return b.downstreamSSEComment }, nil)
}

func (s *Scope) DownstreamBodyEnded(ctx context.Context, value BodyEnded) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[BodyEnded] { return b.downstreamBodyEnded }, nil)
}

func (s *Scope) RequestFinished(ctx context.Context, value RequestFinished) error {
	return dispatch(s, ctx, value, func(b *Binder) []subscription[RequestFinished] { return b.requestFinished }, nil)
}

func (s *Scope) MutateOutboundRequest(ctx context.Context, request *http.Request) error {
	if request == nil {
		return nil
	}
	return mutate(s, ctx, request, func(b *Binder) []mutation[http.Request] { return b.outboundRequest })
}

func (s *Scope) MutateOutboundBody(ctx context.Context, draft *BodyDraft) error {
	return mutate(s, ctx, draft, func(b *Binder) []mutation[BodyDraft] { return b.outboundBody })
}

func (s *Scope) MutateUpstreamResponse(ctx context.Context, draft *ResponseDraft) error {
	return mutate(s, ctx, draft, func(b *Binder) []mutation[ResponseDraft] { return b.upstreamDraft })
}

func (s *Scope) MutateUpstreamBodyChunk(ctx context.Context, draft *BodyDraft) error {
	return mutate(s, ctx, draft, func(b *Binder) []mutation[BodyDraft] { return b.upstreamBodyDraft })
}

func dispatch[T any](s *Scope, ctx context.Context, value T, selectHandlers func(*Binder) []subscription[T], clone func(T) T) error {
	return dispatchAt(s, ctx, time.Time{}, value, selectHandlers, clone)
}

func dispatchAt[T any](s *Scope, ctx context.Context, observedAt time.Time, value T, selectHandlers func(*Binder) []subscription[T], clone func(T) T) error {
	if s == nil || s.closed.Load() {
		return nil
	}
	var result error
	for _, mounted := range s.mounted {
		for _, item := range selectHandlers(&mounted.binder) {
			current := value
			if item.policy.Executor == ExecutorOrderedLane && clone != nil {
				current = clone(value)
			}
			eventCtx := s.eventContextAt(ctx, observedAt, mounted)
			task := func() error { return mounted.call(func() error { return item.handle(eventCtx, current) }) }
			if item.policy.Executor == ExecutorCaller {
				result = errors.Join(result, task())
				continue
			}
			if item.policy.Barrier != BarrierBeforeCommit {
				if err := mounted.lane.submit(ctx, item.policy, task); err != nil {
					result = errors.Join(result, err)
				}
				continue
			}
			completed := make(chan error, 1)
			if err := mounted.lane.submit(ctx, item.policy, func() error {
				err := task()
				completed <- err
				return err
			}); err != nil {
				result = errors.Join(result, err)
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

func mutate[T any](s *Scope, ctx context.Context, value *T, selectHandlers func(*Binder) []mutation[T]) error {
	if s == nil || value == nil || s.closed.Load() {
		return nil
	}
	for _, mounted := range s.mounted {
		for _, item := range selectHandlers(&mounted.binder) {
			eventCtx := s.eventContext(ctx, mounted)
			task := func() error { return mounted.call(func() error { return item.handle(eventCtx, value) }) }
			if item.policy.Executor == ExecutorCaller {
				if err := task(); err != nil {
					return err
				}
				continue
			}
			result := make(chan error, 1)
			if err := mounted.lane.submit(ctx, Policy{Executor: ExecutorOrderedLane, Barrier: BarrierBeforeCommit, Overflow: OverflowBlock}, func() error {
				err := task()
				result <- err
				return err
			}); err != nil {
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

func (s *Scope) eventContext(ctx context.Context, mounted *mountedInstance) EventContext {
	return s.eventContextAt(ctx, time.Time{}, mounted)
}

func (s *Scope) eventContextAt(ctx context.Context, observedAt time.Time, mounted *mountedInstance) EventContext {
	if ctx == nil {
		ctx = context.Background()
	}
	attempt := int(s.attempt.Load())
	var output Output
	if mounted.outputs != nil {
		output = mounted.outputs.Output(mounted.producer, attempt)
	}
	if observedAt.IsZero() {
		observedAt = nowUTC()
	}
	return EventContext{Context: ctx, TraceID: s.context.TraceID, Attempt: attempt, ObservedAt: observedAt, Output: output}
}

func (m *mountedInstance) call(run func() error) (err error) {
	if m == nil || m.failed.Load() {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			m.failed.Store(true)
			if m.outputs != nil {
				m.outputs.ModuleFailed(m.moduleName)
			}
			err = fmt.Errorf("module %q panicked: %v", m.producer, recovered)
		}
	}()
	err = run()
	if err != nil {
		m.failed.Store(true)
		if m.outputs != nil {
			m.outputs.ModuleFailed(m.moduleName)
		}
	}
	return err
}

func newOrderedLane(capacity int) *orderedLane {
	if capacity <= 0 {
		capacity = 128
	}
	lane := &orderedLane{queue: make(chan func() error, capacity), done: make(chan struct{})}
	go lane.run()
	return lane
}

func (l *orderedLane) submit(ctx context.Context, policy Policy, task func() error) error {
	if l == nil || task == nil {
		return nil
	}
	switch policy.Overflow {
	case OverflowDrop:
		select {
		case l.queue <- task:
			return nil
		default:
			return nil
		}
	case OverflowFailRequest:
		select {
		case l.queue <- task:
			return nil
		default:
			return errors.New("module ordered lane is full")
		}
	default:
		select {
		case l.queue <- task:
			return nil
		case <-ctx.Done():
			return ctx.Err()
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

func (b *Binder) needsLane() bool {
	return b.laneCapacity() > 0
}

func (b *Binder) laneCapacity() int {
	capacity := 0
	accept := func(policy Policy) {
		if policy.Executor == ExecutorOrderedLane && policy.Capacity > capacity {
			capacity = policy.Capacity
		}
	}
	for _, item := range b.allPolicies() {
		accept(item)
	}
	return capacity
}

func (b *Binder) allPolicies() []Policy {
	var result []Policy
	appendSubscriptions := func(values ...[]Policy) {
		for _, value := range values {
			result = append(result, value...)
		}
	}
	policies := func(length int, at func(int) Policy) []Policy {
		items := make([]Policy, length)
		for index := range items {
			items[index] = at(index)
		}
		return items
	}
	appendSubscriptions(
		policies(len(b.requestStarted), func(i int) Policy { return b.requestStarted[i].policy }),
		policies(len(b.requestBodyAvailable), func(i int) Policy { return b.requestBodyAvailable[i].policy }),
		policies(len(b.requestBodyEnded), func(i int) Policy { return b.requestBodyEnded[i].policy }),
		policies(len(b.attemptStarted), func(i int) Policy { return b.attemptStarted[i].policy }),
		policies(len(b.directiveResolved), func(i int) Policy { return b.directiveResolved[i].policy }),
		policies(len(b.directiveFailed), func(i int) Policy { return b.directiveFailed[i].policy }),
		policies(len(b.metadataBound), func(i int) Policy { return b.metadataBound[i].policy }),
		policies(len(b.metadataChanged), func(i int) Policy { return b.metadataChanged[i].policy }),
		policies(len(b.upstreamStarted), func(i int) Policy { return b.upstreamStarted[i].policy }),
		policies(len(b.upstreamResponse), func(i int) Policy { return b.upstreamResponse[i].policy }),
		policies(len(b.upstreamBodyChunk), func(i int) Policy { return b.upstreamBodyChunk[i].policy }),
		policies(len(b.upstreamJSONChunk), func(i int) Policy { return b.upstreamJSONChunk[i].policy }),
		policies(len(b.upstreamSSEData), func(i int) Policy { return b.upstreamSSEData[i].policy }),
		policies(len(b.upstreamBodyEnded), func(i int) Policy { return b.upstreamBodyEnded[i].policy }),
		policies(len(b.attemptFinished), func(i int) Policy { return b.attemptFinished[i].policy }),
		policies(len(b.retryRequested), func(i int) Policy { return b.retryRequested[i].policy }),
		policies(len(b.downstreamResponse), func(i int) Policy { return b.downstreamResponse[i].policy }),
		policies(len(b.downstreamBodyChunk), func(i int) Policy { return b.downstreamBodyChunk[i].policy }),
		policies(len(b.downstreamSSEData), func(i int) Policy { return b.downstreamSSEData[i].policy }),
		policies(len(b.downstreamSSEComment), func(i int) Policy { return b.downstreamSSEComment[i].policy }),
		policies(len(b.downstreamBodyEnded), func(i int) Policy { return b.downstreamBodyEnded[i].policy }),
		policies(len(b.requestFinished), func(i int) Policy { return b.requestFinished[i].policy }),
		policies(len(b.outboundRequest), func(i int) Policy { return b.outboundRequest[i].policy }),
		policies(len(b.outboundBody), func(i int) Policy { return b.outboundBody[i].policy }),
		policies(len(b.upstreamDraft), func(i int) Policy { return b.upstreamDraft[i].policy }),
		policies(len(b.upstreamBodyDraft), func(i int) Policy { return b.upstreamBodyDraft[i].policy }),
	)
	return result
}

func cloneRequestStarted(value RequestStarted) RequestStarted {
	value.Header = value.Header.Clone()
	return value
}

func cloneDirectiveResolved(value DirectiveResolved) DirectiveResolved {
	if value.Target != nil {
		target := *value.Target
		value.Target = &target
	}
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneMetadataBound(value MetadataBound) MetadataBound {
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneMetadataChanged(value MetadataChanged) MetadataChanged {
	value.Bound = cloneMetadata(value.Bound)
	value.Observed = cloneMetadata(value.Observed)
	return value
}

func cloneUpstreamStarted(value UpstreamStarted) UpstreamStarted {
	value.Header = value.Header.Clone()
	return value
}

func cloneResponseStarted(value ResponseStarted) ResponseStarted {
	value.Header = value.Header.Clone()
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneBodyChunk(value BodyChunk) BodyChunk {
	value.Data = append([]byte(nil), value.Data...)
	return value
}

func cloneSSEData(value SSEData) SSEData {
	value.Data = append([]byte(nil), value.Data...)
	if value.RetryMillis != nil {
		retry := *value.RetryMillis
		value.RetryMillis = &retry
	}
	return value
}

func cloneRetryRequested(value RetryRequested) RetryRequested {
	value.SelectorMetadata = cloneMetadata(value.SelectorMetadata)
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
