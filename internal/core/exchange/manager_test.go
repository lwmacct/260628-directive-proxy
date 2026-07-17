package exchange

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/capture"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

func TestExchangeLifecycleRunsRecoveryRetryAndEmitsEvents(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 10}, runtime)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?token=secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Idempotency-Key", "lifecycle-test")
	current := manager.Start(req)
	current.ConfigureRecovery(&recovery.Policy{Budget: recovery.Budget{MaxAttempts: 3, MaxElapsed: time.Minute}}, 10, time.Minute)
	if err := current.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{"redact-query":["token"]}`)}}); err != nil {
		t.Fatal(err)
	}
	target := mustURL(t, "https://upstream.example/v1/chat?token=upstream-secret")
	firstCanceled := false
	first, err := current.BeginAttempt(func() { firstCanceled = true }, AttemptSource{Mode: "remote", Backend: "redis", Resource: "routing"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ConfigureModules(nil); err != nil {
		t.Fatal(err)
	}
	first.BindMetadata(requestmeta.Metadata{"X-Dproxy-Request-Id": {"request-1"}})
	first.DirectiveResolved(target, time.Millisecond, "digest-1", false, false)
	if !first.BeginUpstream(req) || !first.BeginRecovery() {
		t.Fatal("first attempt did not enter recovery")
	}
	if err := first.RequestRecoveryRetry(); err != nil || !firstCanceled {
		t.Fatalf("recovery retry was not accepted: canceled=%t err=%v", firstCanceled, err)
	}
	if decision := first.FinishRoundTrip(false, context.Canceled); decision != DecisionRetry {
		t.Fatalf("unexpected first decision: %v", decision)
	}
	second, err := current.BeginAttempt(func() {}, AttemptSource{Mode: "remote", Backend: "redis", Resource: "routing"})
	if err != nil {
		t.Fatal(err)
	}
	_ = second.ConfigureModules(nil)
	second.BindMetadata(requestmeta.Metadata{"X-Dproxy-Request-Id": {"request-2"}})
	second.DirectiveResolved(target, time.Millisecond, "digest-2", false, true)
	if !second.BeginUpstream(req) {
		t.Fatal("second attempt did not enter upstream state")
	}
	if decision := second.FinishRoundTrip(true, nil); decision != DecisionReturn {
		t.Fatalf("unexpected second decision: %v", decision)
	}
	current.Complete()
	runtime.Close()
	if err := dispatcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sawRetry bool
	for _, record := range output.Records() {
		if record.Topic == "capture.retry.requested" && record.Data["trigger"] == "recovery_controller" {
			sawRetry = true
		}
	}
	if !sawRetry {
		t.Fatal("recovery retry event was not captured")
	}
}

func TestRecoveryRetryRequiresIdempotencyKeyForPostAndPatch(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			manager := NewManager(ManagerOptions{MaxAttempts: 3}, nil)
			current := manager.Start(httptest.NewRequest(method, "http://proxy.local/resource", nil))
			attempt, _ := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
			attempt.DirectiveResolved(mustURL(t, "https://upstream.example"), 0, "", false, false)
			attempt.BeginUpstream(nil)
			attempt.BeginRecovery()
			if err := attempt.RequestRecoveryRetry(); err != ErrIdempotencyKeyRequired {
				t.Fatalf("unexpected retry result: %v", err)
			}
			current.Complete()
		})
	}
}

func TestExchangeMetadataCaptureUsesHeaderRedactionPolicy(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, runtime)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	if err := current.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{"redact-headers":["x-dproxy-secret-*"],"redact-query":[]}`)}}); err != nil {
		t.Fatal(err)
	}
	attempt, _ := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
	_ = attempt.ConfigureModules(nil)
	attempt.BindMetadata(requestmeta.Metadata{
		"X-Dproxy-Request-Id": {"request-1"}, "X-Dproxy-Secret-Key": {"secret"},
	})
	current.Complete()
	runtime.Close()
	_ = dispatcher.Close(context.Background())
	for _, record := range output.Records() {
		if record.Topic != "capture.request.metadata.bound" {
			continue
		}
		metadata := record.Data["metadata"].(map[string][]string)
		if metadata["X-Dproxy-Secret-Key"][0] != "<redacted>" || metadata["X-Dproxy-Request-Id"][0] != "request-1" {
			t.Fatalf("unexpected captured metadata: %#v", metadata)
		}
		return
	}
	t.Fatal("metadata capture event was not emitted")
}

func TestModuleRuntimeRejectsUnknownRequestModules(t *testing.T) {
	runtime, dispatcher, _ := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, runtime)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	if err := current.ConfigureRequest([]module.Spec{{ID: "missing", Module: "missing.module", Config: []byte(`{}`)}}); err == nil {
		t.Fatal("unknown request module was accepted")
	}
	current.Complete()
	runtime.Close()
	_ = dispatcher.Close(context.Background())
}

func newCaptureRuntime(t *testing.T) (*module.Runtime, *event.Dispatcher, *recordoutput.Output) {
	t.Helper()
	output := recordoutput.New("memory")
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: output, QueueMaxRecords: 1024, QueueMaxBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := module.NewRuntime([]module.Definition{capture.New()}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, dispatcher, output
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
