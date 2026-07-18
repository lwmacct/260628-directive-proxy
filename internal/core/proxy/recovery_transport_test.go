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
	mu      sync.Mutex
	plans   []*Plan
	policy  *recovery.Policy
	program *program.Executable
	calls   int
}

func (*recoveryPrepared) Kind() string                          { return "remote" }
func (prepared *recoveryPrepared) Program() *program.Executable { return prepared.program }
func (*recoveryPrepared) Source() SourceMetadata {
	return SourceMetadata{Mode: "remote", Backend: "redis", Endpoint: "redis://redis.example/1", Resource: "routing"}
}
func (prepared *recoveryPrepared) Recovery() *recovery.Policy {
	return recovery.ClonePolicy(prepared.policy)
}
func (prepared *recoveryPrepared) ResolveAttempt(context.Context, int) (Resolution, error) {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	index := prepared.calls
	prepared.calls++
	if index >= len(prepared.plans) {
		index = len(prepared.plans) - 1
	}
	plan := ClonePlan(prepared.plans[index])
	plan.Recovery = recovery.ClonePolicy(prepared.policy)
	source := prepared.Source()
	source.PayloadSHA256 = "payload-digest"
	return Resolution{Plan: plan, Source: source}, nil
}

type recoveryControllerFunc func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error)

func (controller recoveryControllerFunc) Decide(ctx context.Context, spec recovery.ControllerSpec, event recovery.Event) (recovery.Decision, error) {
	return controller(ctx, spec, event)
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

func (definition recoveryEventDefinition) Name() string { return "test.recovery.events" }

func (definition recoveryEventDefinition) Compile(json.RawMessage) (module.Binding, error) {
	return recoveryEventBinding{recorder: definition.recorder}, nil
}

func (binding recoveryEventBinding) Scope() module.ScopeKind { return module.ScopeAttempt }

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
	targetTwo := mustProxyURL(t, "https://two.example")
	policy := testRecoveryPolicy()
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{
		{Target: targetOne},
		{Target: targetTwo},
	}}
	inbound, _ := http.NewRequest(http.MethodPost, "http://proxy.local/chat", strings.NewReader("request-body"))
	inbound.Header.Set("Idempotency-Key", "recovery-test")
	runtime, err := program.NewRuntime([]module.Definition{recoveryEventDefinition{recorder: recorder}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, runtime)
	current := manager.Start(inbound)
	if err := current.ConfigureProgram(prepared.Program()); err != nil {
		t.Fatal(err)
	}
	var calls int
	var seenBodies []string
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		seenBodies = append(seenBodies, string(body))
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
	controller := recoveryControllerFunc(func(_ context.Context, _ recovery.ControllerSpec, event recovery.Event) (recovery.Decision, error) {
		callbackEvent = event
		return recovery.Decision{Action: recovery.ActionRetry}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{
		RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute,
		MaxRecoveryCallbackTimeout: time.Second, MaxRecoveryBodyBytes: 1 << 20,
	})
	response, err := transport.RoundTrip(inbound.Clone(recoveryTestContext(t, inbound, current, prepared)))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(data) != "ok" || calls != 2 || prepared.calls != 2 {
		t.Fatalf("unexpected recovery result: status=%d body=%q calls=%d resolves=%d", response.StatusCode, data, calls, prepared.calls)
	}
	if len(seenBodies) != 2 || seenBodies[0] != "request-body" || seenBodies[1] != "request-body" {
		t.Fatalf("request body was not replayed: %#v", seenBodies)
	}
	decoded, err := base64.StdEncoding.DecodeString(callbackEvent.Response.Body.Data)
	if err != nil || string(decoded) != `{"error":"expired"}` || callbackEvent.Response.Headers.Get("X-Reason") != "expired" {
		t.Fatalf("unexpected callback response capture: event=%#v decoded=%q err=%v", callbackEvent, decoded, err)
	}
	if callbackEvent.TraceID != current.TraceID() || callbackEvent.Attempt.Number != 1 || callbackEvent.Trigger.Type != recovery.TriggerUnexpectedStatus {
		t.Fatalf("unexpected callback identity: %#v", callbackEvent)
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
		finished.Action != lifecycle.RecoveryActionRetry || finished.NextAttempt != 2 {
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
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime([]module.Definition{recoveryEventDefinition{recorder: recorder}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, runtime)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"X-Test": {"one"}}, Body: io.NopCloser(strings.NewReader("small-error")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionForward}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime([]module.Definition{recoveryEventDefinition{recorder: recorder}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, runtime)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fallback")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{}, errors.New("controller unavailable")
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime([]module.Definition{recoveryEventDefinition{recorder: recorder}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, runtime)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fallback")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionForward, AfterMS: -1}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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
	policy.Budget.MaxAttempts = 1
	recorder := &recoveryEventRecorder{}
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime([]module.Definition{recoveryEventDefinition{recorder: recorder}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, runtime)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fallback")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionRetry}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	runtime, err := program.NewRuntime([]module.Definition{recoveryEventDefinition{recorder: recorder}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	prepared.program = compileRecoveryProgram(t, runtime)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, runtime)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("upstream failed")), Request: request}, nil
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionFail}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, nil)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})
	controller := recoveryControllerFunc(func(context.Context, recovery.ControllerSpec, recovery.Event) (recovery.Decision, error) {
		return recovery.Decision{Action: recovery.ActionFail}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}, {Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, nil)
	current := manager.Start(inbound)
	_ = current.ConfigureProgram(prepared.Program())
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
	controller := recoveryControllerFunc(func(_ context.Context, _ recovery.ControllerSpec, event recovery.Event) (recovery.Decision, error) {
		trigger = event.Trigger.Type
		return recovery.Decision{Action: recovery.ActionRetry}, nil
	})
	transport, _ := NewRecoveryTransport(base, RecoveryTransportOptions{RecoveryController: controller, MaxRecoveryAttempts: 5, MaxRecoveryElapsed: time.Minute})
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

func TestPlanFingerprintIncludesRecoveryPolicy(t *testing.T) {
	target := mustProxyURL(t, "https://upstream.example")
	first := &Plan{Target: target, Recovery: testRecoveryPolicy()}
	second := ClonePlan(first)
	second.Recovery.Budget.MaxAttempts++
	if planFingerprint(first) == planFingerprint(second) {
		t.Fatal("recovery policy did not affect fingerprint")
	}
}

func testRecoveryPolicy() *recovery.Policy {
	controllerURL, _ := url.Parse("https://controller.example/recovery")
	return &recovery.Policy{
		Controller: recovery.ControllerSpec{URL: controllerURL, Timeout: time.Second},
		Triggers: recovery.TriggerPolicy{UnexpectedStatus: &recovery.UnexpectedStatusPolicy{
			Expected: []recovery.StatusRange{{From: 200, To: 299}}, CaptureBodyBytes: 64 << 10,
		}},
		Budget: recovery.Budget{MaxAttempts: 3, MaxElapsed: time.Minute},
	}
}

func compileRecoveryProgram(t *testing.T, runtime *program.Runtime) *program.Executable {
	t.Helper()
	executable, err := runtime.Compile(program.Program{Attempt: []program.Spec{{ID: "events", Module: "test.recovery.events"}}})
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func recoveryTestContext(t *testing.T, inbound *http.Request, current *exchange.Exchange, prepared PreparedDirective) context.Context {
	t.Helper()
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
