package proxy

import (
	"context"
	"encoding/base64"
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
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

type recoveryPrepared struct {
	mu     sync.Mutex
	plans  []*Plan
	policy *recovery.Policy
	calls  int
}

func (*recoveryPrepared) Kind() string                  { return "remote" }
func (*recoveryPrepared) RequestProgram() []module.Spec { return nil }
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

func TestRecoveryTransportRetriesAfterUnexpectedStatus(t *testing.T) {
	targetOne := mustProxyURL(t, "https://one.example")
	targetTwo := mustProxyURL(t, "https://two.example")
	policy := testRecoveryPolicy()
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: targetOne}, {Target: targetTwo}}}
	inbound, _ := http.NewRequest(http.MethodPost, "http://proxy.local/chat", strings.NewReader("request-body"))
	inbound.Header.Set("Idempotency-Key", "recovery-test")
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, nil)
	current := manager.Start(inbound)
	if err := current.ConfigureRequest(nil); err != nil {
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
	current.Complete()
}

func TestRecoveryTransportForwardsCapturedResponseWhenControllerSaysForward(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, nil)
	current := manager.Start(inbound)
	_ = current.ConfigureRequest(nil)
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
}

func TestRecoveryTransportFailsUnexpectedResponseWhenControllerSaysFail(t *testing.T) {
	target := mustProxyURL(t, "https://one.example")
	policy := testRecoveryPolicy()
	prepared := &recoveryPrepared{policy: policy, plans: []*Plan{{Target: target}}}
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 5}, nil)
	current := manager.Start(inbound)
	_ = current.ConfigureRequest(nil)
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
	_ = current.ConfigureRequest(nil)
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
	_ = current.ConfigureRequest(nil)
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
