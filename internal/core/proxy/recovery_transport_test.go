package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

type recoveryPrepared struct {
	plan    *Plan
	policy  *recovery.Policy
	program *program.Executable
}

func (prepared *recoveryPrepared) Program() *program.Executable { return prepared.program }
func (prepared *recoveryPrepared) Prepared(t *testing.T) *PreparedDirective {
	t.Helper()
	value, err := NewPreparedDirective(DirectiveSource{
		Mode: "remote", Backend: "redis", Endpoint: "redis://redis.example/1", Resource: "routing",
		PayloadSHA256: "payload-digest",
	}, prepared.plan, prepared.program, prepared.policy, proxyTestMetadata())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type recoveryControllerFunc func(context.Context, recovery.Event) (recovery.Decision, error)

func (controller recoveryControllerFunc) Decide(ctx context.Context, event recovery.Event) (recovery.Decision, error) {
	return controller(ctx, event)
}

type recoveryEventRecorder struct {
	mu        sync.Mutex
	phases    []string
	sequences []uint64
	eventIDs  []string
	started   lifecycle.RecoveryStarted
	decided   lifecycle.RecoveryDecided
	finished  lifecycle.RecoveryFinished
}

type recoveryEventDefinition struct{ recorder *recoveryEventRecorder }

type recoveryEventBinding struct{ recorder *recoveryEventRecorder }

type recoveryEventInstance struct{ recorder *recoveryEventRecorder }

func (definition recoveryEventDefinition) Name() string              { return "test.recovery.events" }
func (definition recoveryEventDefinition) Lifetime() module.Lifetime { return module.LifetimeRoundTrip }

func (definition recoveryEventDefinition) CompileProgram(_ json.RawMessage) (module.Binding, error) {
	return recoveryEventBinding(definition), nil
}

func (binding recoveryEventBinding) Open(module.OpenContext) (module.Instance, error) {
	return &recoveryEventInstance{recorder: binding.recorder}, nil
}

func (instance *recoveryEventInstance) Bind(binder module.Registrar) {
	binder.OnRecoveryStarted(module.SyncPolicy(), func(ctx module.Context, value lifecycle.RecoveryStarted) error {
		instance.recorder.mu.Lock()
		defer instance.recorder.mu.Unlock()
		instance.recorder.phases = append(instance.recorder.phases, "started")
		instance.recorder.sequences = append(instance.recorder.sequences, ctx.Sequence)
		instance.recorder.eventIDs = append(instance.recorder.eventIDs, ctx.EventID)
		instance.recorder.started = value
		return nil
	})
	binder.OnRecoveryDecided(module.SyncPolicy(), func(ctx module.Context, value lifecycle.RecoveryDecided) error {
		instance.recorder.mu.Lock()
		defer instance.recorder.mu.Unlock()
		instance.recorder.phases = append(instance.recorder.phases, "decided")
		instance.recorder.sequences = append(instance.recorder.sequences, ctx.Sequence)
		instance.recorder.eventIDs = append(instance.recorder.eventIDs, ctx.EventID)
		instance.recorder.decided = value
		return nil
	})
	binder.OnRecoveryFinished(module.SyncPolicy(), func(ctx module.Context, value lifecycle.RecoveryFinished) error {
		instance.recorder.mu.Lock()
		defer instance.recorder.mu.Unlock()
		instance.recorder.phases = append(instance.recorder.phases, "finished")
		instance.recorder.sequences = append(instance.recorder.sequences, ctx.Sequence)
		instance.recorder.eventIDs = append(instance.recorder.eventIDs, ctx.EventID)
		instance.recorder.finished = value
		return nil
	})
}

func (*recoveryEventInstance) Finish(module.FinishContext) error { return nil }

func TestRecoveryTransportRetriesAfterUnexpectedStatus(t *testing.T) {
	targetOne := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: targetOne}}
	inbound, _ := http.NewRequest(http.MethodPost, "http://proxy.local/chat", strings.NewReader("request-body"))
	inbound.Header.Set("Idempotency-Key", "recovery-test")
	runtime, err := program.NewRuntime(module.MustCatalog(recoveryEventDefinition{recorder: recorder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, runtime)
	current := manager.Start(inbound)
	var calls int
	var seenBodies []string
	var seenTargets []string
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		seenBodies = append(seenBodies, string(body))
		seenTargets = append(seenTargets, request.URL.String())
		calls++
		if calls == 1 {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": {"application/json"}, "X-Reason": {"expired"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":"expired"}`)), Request: request,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
	})
	var callbackEvent recovery.Event
	controller := recoveryControllerFunc(func(_ context.Context, event recovery.Event) (recovery.Decision, error) {
		callbackEvent = event
		return recovery.Decision{Action: recovery.ActionRetry}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{
		MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute,
		MaxRecoveryBodyBytes: 1 << 20,
	})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(data) != "ok" || calls != 2 {
		t.Fatalf("unexpected recovery result: status=%d body=%q calls=%d", response.StatusCode, data, calls)
	}
	if len(seenBodies) != 2 || seenBodies[0] != "request-body" || seenBodies[1] != "request-body" {
		t.Fatalf("request body was not replayed: %#v", seenBodies)
	}
	if len(seenTargets) != 2 || seenTargets[0] != "https://one.example" || seenTargets[1] != "https://one.example" {
		t.Fatalf("recovery did not reuse the prepared target: %#v", seenTargets)
	}
	decoded, err := base64.StdEncoding.DecodeString(callbackEvent.Response.Body.Data)
	if err != nil || string(decoded) != `{"error":"expired"}` || callbackEvent.Response.Headers.Get("X-Reason") != "expired" {
		t.Fatalf("unexpected callback response capture: event=%#v decoded=%q err=%v", callbackEvent, decoded, err)
	}
	if callbackEvent.TraceID != current.TraceID() || callbackEvent.RoundTrip.Number != 1 || callbackEvent.Trigger.Type != recovery.TriggerUnexpectedStatus {
		t.Fatalf("unexpected callback identity: %#v", callbackEvent)
	}
	if callbackEvent.Metadata["user_key"] != "uk_test" {
		t.Fatalf("callback metadata was not attached: %#v", callbackEvent.Metadata)
	}
	if callbackEvent.Directive.Endpoint != "redis://redis.example/1" || callbackEvent.Directive.Resource != "routing" {
		t.Fatalf("unexpected directive source: %#v", callbackEvent.Directive)
	}
	recorder.mu.Lock()
	phases := append([]string(nil), recorder.phases...)
	started := recorder.started
	decided := recorder.decided
	finished := recorder.finished
	sequences := append([]uint64(nil), recorder.sequences...)
	eventIDs := append([]string(nil), recorder.eventIDs...)
	recorder.mu.Unlock()
	if strings.Join(phases, ",") != "started,decided,finished" {
		t.Fatalf("unexpected module recovery event sequence: %#v", phases)
	}
	if started.EventID != callbackEvent.EventID || started.Trigger != string(callbackEvent.Trigger.Type) ||
		started.Response == nil || started.Response.StatusCode != http.StatusUnauthorized ||
		started.Response.Body == nil || started.Response.Body.Data != callbackEvent.Response.Body.Data {
		t.Fatalf("unexpected recovery started event: %#v", started)
	}
	if decided.EventID != started.EventID || decided.Action != lifecycle.RecoveryActionRetry {
		t.Fatalf("unexpected recovery decided event: %#v", decided)
	}
	if finished.EventID != started.EventID || finished.Outcome != lifecycle.RecoveryOutcomeRetryRequested ||
		finished.Action != lifecycle.RecoveryActionRetry || finished.NextRoundTrip != 2 {
		t.Fatalf("unexpected recovery finished event: %#v", finished)
	}
	if len(sequences) != 3 || sequences[0] == 0 || sequences[0] >= sequences[1] || sequences[1] >= sequences[2] ||
		strings.Join(eventIDs, ",") != strings.Join([]string{started.EventID, started.EventID, started.EventID}, ",") {
		t.Fatalf("unexpected lifecycle event metadata: sequences=%#v event_ids=%#v", sequences, eventIDs)
	}
	current.Complete()
}

func TestRecoveryTransportForwardsCapturedResponseWhenControllerSaysForward(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime(module.MustCatalog(recoveryEventDefinition{recorder: recorder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, runtime)
	current := manager.Start(inbound)
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"X-Test": {"one"}}, Body: io.NopCloser(strings.NewReader("small-error")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionForward}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadGateway || string(data) != "small-error" || response.Header.Get("X-Test") != "one" {
		t.Fatalf("captured response was not preserved: status=%d headers=%#v body=%q", response.StatusCode, response.Header, data)
	}
	current.Complete()
	recorder.mu.Lock()
	phases := strings.Join(recorder.phases, ",")
	finished := recorder.finished
	recorder.mu.Unlock()
	if phases != "started,decided,finished" || finished.Outcome != lifecycle.RecoveryOutcomeForwarded ||
		finished.Action != lifecycle.RecoveryActionForward {
		t.Fatalf("unexpected forward recovery events: phases=%s finished=%#v", phases, finished)
	}
}

func TestRecoveryTransportReportsControllerError(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime(module.MustCatalog(recoveryEventDefinition{recorder: recorder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, runtime)
	current := manager.Start(inbound)
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fallback")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{}, errors.New("controller unavailable")
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil || response == nil {
		t.Fatalf("unexpected controller error fallback: response=%#v err=%v", response, err)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	current.Complete()
	recorder.mu.Lock()
	phases := strings.Join(recorder.phases, ",")
	finished := recorder.finished
	recorder.mu.Unlock()
	if phases != "started,finished" || finished.Outcome != lifecycle.RecoveryOutcomeControllerError ||
		finished.ErrorCode != "controller_error" || !strings.Contains(finished.Error, "controller unavailable") {
		t.Fatalf("unexpected controller error events: phases=%s finished=%#v", phases, finished)
	}
}

func TestRecoveryTransportReportsInvalidDecision(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime(module.MustCatalog(recoveryEventDefinition{recorder: recorder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, runtime)
	current := manager.Start(inbound)
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fallback")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionForward, AfterMS: -1}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil || response == nil {
		t.Fatalf("unexpected invalid decision fallback: response=%#v err=%v", response, err)
	}
	_ = response.Body.Close()
	current.Complete()
	recorder.mu.Lock()
	phases := strings.Join(recorder.phases, ",")
	finished := recorder.finished
	recorder.mu.Unlock()
	if phases != "started,finished" || finished.Outcome != lifecycle.RecoveryOutcomeInvalidDecision ||
		finished.ErrorCode != lifecycle.RecoveryErrorCodeInvalidDecision {
		t.Fatalf("unexpected invalid decision events: phases=%s finished=%#v", phases, finished)
	}
}

func TestRecoveryTransportReportsBudgetRejection(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	policy.Budget.MaxRoundTrips = 1
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime(module.MustCatalog(recoveryEventDefinition{recorder: recorder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, runtime)
	current := manager.Start(inbound)
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fallback")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionRetry}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil || response == nil {
		t.Fatalf("unexpected budget rejection fallback: response=%#v err=%v", response, err)
	}
	_ = response.Body.Close()
	current.Complete()
	recorder.mu.Lock()
	phases := strings.Join(recorder.phases, ",")
	finished := recorder.finished
	recorder.mu.Unlock()
	if phases != "started,decided,finished" || finished.Outcome != lifecycle.RecoveryOutcomeBudgetRejected ||
		finished.ErrorCode != lifecycle.RecoveryErrorCodeRetryNotAllowed {
		t.Fatalf("unexpected budget rejection events: phases=%s finished=%#v", phases, finished)
	}
}

func TestRecoveryTransportFailsUnexpectedResponseWhenControllerSaysFail(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime(module.MustCatalog(recoveryEventDefinition{recorder: recorder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, runtime)
	current := manager.Start(inbound)
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("upstream failed")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionFail}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if response != nil || !errors.Is(err, ErrRecoveryFailed) {
		t.Fatalf("unexpected fail decision result: response=%#v err=%v", response, err)
	}
	current.Complete()
	recorder.mu.Lock()
	phases := strings.Join(recorder.phases, ",")
	finished := recorder.finished
	recorder.mu.Unlock()
	if phases != "started,decided,finished" || finished.Outcome != lifecycle.RecoveryOutcomeFailed ||
		finished.Action != lifecycle.RecoveryActionFail || finished.ErrorCode != "controller_fail" {
		t.Fatalf("unexpected fail recovery events: phases=%s finished=%#v", phases, finished)
	}
}

func TestRecoveryTransportFailsTransportErrorWhenControllerSaysFail(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	policy.Triggers.UnexpectedStatus = nil
	policy.Triggers.TransportError = true
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, nil)
	current := manager.Start(inbound)
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionFail}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if response != nil || !errors.Is(err, ErrRecoveryFailed) {
		t.Fatalf("unexpected fail decision result: response=%#v err=%v", response, err)
	}
	current.Complete()
}

func TestRecoveryTransportRecoversAfterResponseHeaderTimeout(t *testing.T) {
	target := mustProxyURL(t, "https://slow.example")
	policy := testRecoveryPolicy()
	policy.Triggers.UnexpectedStatus = nil
	policy.Triggers.ResponseHeaderTimeout = 20 * time.Millisecond
	prepared := &recoveryPrepared{policy: policy, plan: &Plan{Target: target}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 5}, nil)
	current := manager.Start(inbound)
	var calls int
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.WroteRequest != nil {
				trace.WroteRequest(httptrace.WroteRequestInfo{})
			}
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	})
	var trigger recovery.TriggerType
	controller := recoveryControllerFunc(func(_ context.Context, event recovery.Event) (recovery.Decision, error) {
		trigger = event.Trigger.Type
		return recovery.Decision{Action: recovery.ActionRetry}, nil
	})
	policy.Controller = controller
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{MaxRecoveryRoundTrips: 5, MaxRecoveryElapsed: time.Minute})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent || calls != 2 || trigger != recovery.TriggerResponseHeaderTimeout {
		t.Fatalf("unexpected timeout recovery: status=%d calls=%d trigger=%s", response.StatusCode, calls, trigger)
	}
	current.Complete()
}

func TestPreparedDirectiveClonesPlanAndRecovery(t *testing.T) {
	target := mustProxyURL(t, "https://upstream.example")
	prepared, err := NewPreparedDirective(DirectiveSource{Mode: "inline"}, &Plan{Target: target}, nil, testRecoveryPolicy(), proxyTestMetadata())
	if err != nil {
		t.Fatal(err)
	}
	plan := prepared.Plan()
	policy := prepared.Recovery()
	plan.Target.Host = "mutated.example"
	policy.Budget.MaxRoundTrips++
	if prepared.Plan().Target.Host != "upstream.example" || prepared.Recovery().Budget.MaxRoundTrips == policy.Budget.MaxRoundTrips {
		t.Fatal("prepared plan or recovery policy exposed mutable state")
	}
}

func testRecoveryPolicy() *recovery.Policy {
	return &recovery.Policy{
		Controller: recoveryControllerFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
			return recovery.Decision{Action: recovery.ActionFail}, nil
		}),
		Triggers: recovery.TriggerPolicy{UnexpectedStatus: &recovery.UnexpectedStatusPolicy{
			Expected: []recovery.StatusRange{{From: 200, To: 299}}, CaptureBodyBytes: 64 << 10,
		}},
		Budget: recovery.Budget{MaxRoundTrips: 3, MaxElapsed: time.Minute},
	}
}

func compileRecoveryProgram(t *testing.T, runtime *program.Runtime) *program.Executable {
	t.Helper()
	executable, err := runtime.Compile(module.Specs{{Module: "test.recovery.events"}})
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func recoveryTestContext(t *testing.T, inbound *http.Request, current *exchange.Exchange, fixture *recoveryPrepared) context.Context {
	t.Helper()
	prepared := fixture.Prepared(t)
	plan := prepared.Plan()
	source := prepared.Source()
	if err := current.Configure(exchange.Configuration{
		Directive: exchange.DirectiveInfo{
			Mode: source.Mode, Backend: source.Backend, Endpoint: source.Endpoint, Resource: source.Resource,
			PayloadSHA256: source.PayloadSHA256, Duration: source.Duration, Target: plan.Target,
		},
		Metadata: prepared.Metadata(), Program: prepared.Program(),
	}); err != nil {
		t.Fatal(err)
	}
	var data []byte
	if inbound.Body != nil && inbound.Body != http.NoBody {
		var err error
		data, err = io.ReadAll(inbound.Body)
		if err != nil {
			t.Fatal(err)
		}
		_ = inbound.Body.Close()
		inbound.Body = http.NoBody
	}
	controller := bodystore.New(bodystore.Config{
		MemoryMaxBytes: 1 << 20, MemoryPerBodyBytes: 1 << 20, DiskMaxBytes: 1 << 20,
		MaxBodyBytes: 1 << 20, ChunkBytes: 4 << 10, TempDir: t.TempDir(),
	})
	body, err := controller.Stream(t.Context(), io.NopCloser(strings.NewReader(string(data))), int64(len(data)), bodystore.Observer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = body.Close() })
	ctx := exchange.ContextWithExchange(inbound.Context(), current)
	return contextWithPreparedRequest(ctx, prepared, NewRequestTemplate(inbound), body)
}

func mustProxyURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
