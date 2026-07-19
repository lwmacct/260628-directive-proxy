package exchange

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/capture"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

type recordingMetrics struct {
	roundTripsStarted int
	roundTripOutcomes []string
	recoveryTriggers  []string
	recoveryOutcomes  []string
}

func (metrics *recordingMetrics) RoundTripStarted() {
	metrics.roundTripsStarted++
}

func (metrics *recordingMetrics) RoundTripFinished(outcome string, _ time.Duration) {
	metrics.roundTripOutcomes = append(metrics.roundTripOutcomes, outcome)
}

func (metrics *recordingMetrics) RecoveryStarted(trigger string) {
	metrics.recoveryTriggers = append(metrics.recoveryTriggers, trigger)
}

func (metrics *recordingMetrics) RecoveryFinished(outcome string) {
	metrics.recoveryOutcomes = append(metrics.recoveryOutcomes, outcome)
}

func TestExchangeLifecycleRunsRecoveryRetryAndEmitsEvents(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	metrics := &recordingMetrics{}
	manager := NewManager(ManagerOptions{MaxRoundTrips: 10, Metrics: metrics}, runtime)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?token=secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Idempotency-Key", "lifecycle-test")
	current := manager.Start(req)
	current.ConfigureRecovery(&recovery.Policy{Budget: recovery.Budget{MaxRoundTrips: 3, MaxElapsed: time.Minute}}, 10, time.Minute)
	executable := compileProgram(t, runtime, module.Specs{{Module: capture.Name, Config: []byte(`{"redact-query":["token"]}`)}})
	target := mustURL(t, "https://upstream.example/v1/chat?token=upstream-secret")
	if err := current.Configure(Configuration{
		Directive: DirectiveInfo{Mode: "remote", Backend: "redis", Resource: "routing", PayloadSHA256: "digest-1", Target: target},
		Metadata:  exchangeMetadata(t, map[string]string{"user_key": "uk_user_1", "request_id": "request-1"}), Program: executable,
	}); err != nil {
		t.Fatal(err)
	}
	firstCanceled := false
	first, err := current.BeginRoundTrip(func() { firstCanceled = true })
	if err != nil {
		t.Fatal(err)
	}
	if err := first.OpenScope(); err != nil {
		t.Fatal(err)
	}
	if !first.BeginUpstream(req) || !first.BeginRecovery() {
		t.Fatal("first roundTrip did not enter recovery")
	}
	first.RecoveryStarted(lifecycle.RecoveryStarted{EventID: "trace:1:unexpected_status", Trigger: "unexpected_status"})
	first.RecoveryDecided(lifecycle.RecoveryDecided{EventID: "trace:1:unexpected_status", Action: lifecycle.RecoveryActionRetry})
	if err := first.RequestRecoveryRetry(); err != nil || !firstCanceled {
		t.Fatalf("recovery retry was not accepted: canceled=%t err=%v", firstCanceled, err)
	}
	first.RecoveryFinished(lifecycle.RecoveryFinished{
		EventID: "trace:1:unexpected_status", Outcome: lifecycle.RecoveryOutcomeRetryRequested,
		Action: lifecycle.RecoveryActionRetry, NextRoundTrip: 2,
	})
	if decision := first.FinishRoundTrip(false, context.Canceled); decision != DecisionRetry {
		t.Fatalf("unexpected first decision: %v", decision)
	}
	second, err := current.BeginRoundTrip(func() {})
	if err != nil {
		t.Fatal(err)
	}
	_ = second.OpenScope()
	if !second.BeginUpstream(req) {
		t.Fatal("second roundTrip did not enter upstream state")
	}
	if decision := second.FinishRoundTrip(true, nil); decision != DecisionReturn {
		t.Fatalf("unexpected second decision: %v", decision)
	}
	current.Complete()
	runtime.Close()
	if err := dispatcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var recoveryTopics []string
	preparedCount := 0
	roundTripStartedCount := 0
	for _, record := range output.Records() {
		if record.Topic == "capture.directive.prepared" {
			preparedCount++
			if record.Data["payload_sha256"] != "digest-1" {
				t.Fatalf("unexpected prepared directive: %#v", record.Data)
			}
		}
		if record.Topic == "capture.round_trip.started" {
			roundTripStartedCount++
			if record.Producer != capture.Name || record.RoundTrip == 0 || record.Data["round_trip"] != record.RoundTrip ||
				record.Data["payload_sha256"] != "digest-1" || record.Metadata["request_id"] != "request-1" {
				t.Fatalf("roundTrip did not reuse prepared directive: %#v", record.Data)
			}
		}
		if record.Metadata["user_key"] != "uk_user_1" || record.TraceID != current.TraceID() {
			t.Fatalf("record did not carry exchange metadata: %#v", record)
		}
		if record.Topic == "capture.recovery.started" || record.Topic == "capture.recovery.decided" || record.Topic == "capture.recovery.finished" {
			recoveryTopics = append(recoveryTopics, record.Topic)
		}
		if record.Topic == "capture.recovery.finished" && record.Data["outcome"] != string(lifecycle.RecoveryOutcomeRetryRequested) {
			t.Fatalf("unexpected recovery finish: %#v", record.Data)
		}
	}
	if got := strings.Join(recoveryTopics, ","); got != "capture.recovery.started,capture.recovery.decided,capture.recovery.finished" {
		t.Fatalf("unexpected recovery event sequence: %s", got)
	}
	if preparedCount != 1 || roundTripStartedCount != 2 {
		t.Fatalf("unexpected directive lifecycle counts: prepared=%d roundTrips=%d", preparedCount, roundTripStartedCount)
	}
	if metrics.roundTripsStarted != 2 || strings.Join(metrics.roundTripOutcomes, ",") != "canceled_for_retry,completed" ||
		strings.Join(metrics.recoveryTriggers, ",") != "unexpected_status" || strings.Join(metrics.recoveryOutcomes, ",") != "retry_requested" {
		t.Fatalf("unexpected lifecycle metrics: %#v", metrics)
	}
}

func TestRecoveryRetryRequiresIdempotencyKeyForPostAndPatch(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			manager := NewManager(ManagerOptions{MaxRoundTrips: 3}, nil)
			current := manager.Start(httptest.NewRequest(method, "http://proxy.local/resource", nil))
			prepareInlineExchange(t, current)
			roundTrip, _ := current.BeginRoundTrip(func() {})
			roundTrip.BeginUpstream(nil)
			roundTrip.BeginRecovery()
			if err := roundTrip.RequestRecoveryRetry(); err != ErrIdempotencyKeyRequired {
				t.Fatalf("unexpected retry result: %v", err)
			}
			current.Complete()
		})
	}
}

func TestExchangeMetadataIsAttachedToEveryRecord(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxRoundTrips: 2}, runtime)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	executable := compileProgram(t, runtime, module.Specs{{Module: capture.Name, Config: []byte(`{}`)}})
	if err := current.Configure(Configuration{
		Directive: DirectiveInfo{Mode: "inline", Target: mustURL(t, "https://upstream.example")},
		Metadata:  exchangeMetadata(t, map[string]string{"user_key": "uk_user_1", "tenant_id": "tenant-a"}), Program: executable,
	}); err != nil {
		t.Fatal(err)
	}
	roundTrip, _ := current.BeginRoundTrip(func() {})
	_ = roundTrip.OpenScope()
	current.Complete()
	runtime.Close()
	_ = dispatcher.Close(context.Background())
	records := output.Records()
	if len(records) == 0 {
		t.Fatal("capture emitted no records")
	}
	for _, record := range records {
		if record.Metadata["user_key"] != "uk_user_1" || record.Metadata["tenant_id"] != "tenant-a" || record.TraceID != current.TraceID() {
			t.Fatalf("record missing metadata: %#v", record)
		}
	}
}

func TestModuleRuntimeRejectsUnknownModules(t *testing.T) {
	runtime, dispatcher, _ := newCaptureRuntime(t)
	if _, err := runtime.Compile(module.Specs{{Module: "missing.module", Config: []byte(`{}`)}}); err == nil {
		t.Fatal("unknown module was accepted")
	}
	runtime.Close()
	_ = dispatcher.Close(context.Background())
}

func newCaptureRuntime(t *testing.T) (*program.Runtime, *event.Dispatcher, *recordoutput.Output) {
	t.Helper()
	output := recordoutput.New("memory")
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: output, QueueMaxRecords: 1024, QueueMaxBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := program.NewRuntime(module.MustCatalog(capture.New()), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, dispatcher, output
}

func compileProgram(t *testing.T, runtime *program.Runtime, source module.Specs) *program.Executable {
	t.Helper()
	executable, err := runtime.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func prepareInlineExchange(t *testing.T, current *Exchange) {
	t.Helper()
	if err := current.Configure(Configuration{
		Directive: DirectiveInfo{Mode: "inline", Target: mustURL(t, "https://upstream.example")},
		Metadata:  exchangeMetadata(t, map[string]string{"user_key": "uk_test"}),
	}); err != nil {
		t.Fatal(err)
	}
}

func exchangeMetadata(t *testing.T, input map[string]string) metadata.Set {
	t.Helper()
	fields, err := metadata.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}
