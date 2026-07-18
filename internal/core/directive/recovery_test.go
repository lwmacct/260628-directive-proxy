package directive

import (
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/adapter/recoveryhttp"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

func TestRecoveryPayloadRoundTripAndCompile(t *testing.T) {
	payload := Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{BaseURL: "https://api.example.com"},
		Recovery: &RecoverySpec{
			Controller: recovery.ControllerSpec{
				URL: "https://control.example.com/recovery",
				Headers: map[string]string{
					"authorization": "Bearer secret",
				},
			},
			Triggers: RecoveryTriggerSpec{
				ResponseHeaderTimeout: "10s",
				UnexpectedStatus:      &RecoveryUnexpectedStatusSpec{Expected: []RecoveryStatusRangeSpec{{From: 200, To: 299}}},
				TransportError:        true,
			},
			Budget: RecoveryBudgetSpec{MaxRoundTrips: 3},
		},
	}
	token, err := Encode(testTokenSecret, payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(testTokenSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Payload == nil || decoded.Payload.Recovery == nil {
		t.Fatalf("missing recovery payload: %#v", decoded)
	}
	spec := decoded.Payload.Recovery
	if spec.Controller.URL != "https://control.example.com/recovery" || spec.Controller.Timeout != "3s" ||
		spec.Controller.Headers["Authorization"] != "Bearer secret" || spec.Budget.MaxElapsed != "30s" ||
		spec.Triggers.UnexpectedStatus.CaptureBodyBytes != 64<<10 {
		t.Fatalf("unexpected normalized recovery payload: %#v", spec)
	}
	compiler := recoveryhttp.New(recoveryhttp.Options{})
	defer func() { _ = compiler.Close() }()
	compiled, err := CompileRecovery(spec, compiler)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Budget.MaxRoundTrips != 3 || compiled.Budget.MaxElapsed != 30*time.Second ||
		compiled.Triggers.ResponseHeaderTimeout != 10*time.Second || !compiled.Triggers.UnexpectedStatus.Matches(401) ||
		compiled.Triggers.UnexpectedStatus.Matches(204) || compiled.Controller == nil {
		t.Fatalf("unexpected compiled recovery policy: %#v", compiled)
	}
	observation := compiled.Controller.(recovery.ObservableControllerBinding).Observation()
	if observation.Endpoint != "https://control.example.com/recovery" || observation.Timeout != 3*time.Second ||
		observation.Headers.Get("Authorization") != "Bearer secret" {
		t.Fatalf("unexpected compiled recovery controller: %#v", observation)
	}
}

func TestRecoveryValidationRejectsInvalidPolicies(t *testing.T) {
	valid := func() *RecoverySpec {
		return &RecoverySpec{
			Controller: recovery.ControllerSpec{URL: "https://control.example.com/recovery"},
			Triggers: RecoveryTriggerSpec{UnexpectedStatus: &RecoveryUnexpectedStatusSpec{
				Expected: []RecoveryStatusRangeSpec{{From: 200, To: 299}},
			}},
			Budget: RecoveryBudgetSpec{MaxRoundTrips: 3},
		}
	}
	for name, mutate := range map[string]func(*RecoverySpec){
		"controller URL":     func(spec *RecoverySpec) { spec.Controller.URL = "" },
		"controller timeout": func(spec *RecoverySpec) { spec.Controller.Timeout = "11m" },
		"empty triggers":     func(spec *RecoverySpec) { spec.Triggers = RecoveryTriggerSpec{} },
		"status range":       func(spec *RecoverySpec) { spec.Triggers.UnexpectedStatus.Expected[0].From = 100 },
		"round-trip budget":  func(spec *RecoverySpec) { spec.Budget.MaxRoundTrips = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			spec := valid()
			mutate(spec)
			if err := Validate(Payload{Metadata: testDirectiveMetadata(), Target: TargetSection{BaseURL: "https://api.example.com"}, Recovery: spec}); err == nil {
				t.Fatal("invalid recovery policy was accepted")
			}
		})
	}
}

func TestRecoveryRejectsUnknownControllerFields(t *testing.T) {
	raw := []byte(`{"target":{"base_url":"https://api.example.com"},"recovery":{"controller":{"url":"https://control.example.com","unknown":true},"triggers":{"transport_error":true},"budget":{"max_round_trips":2}}}`)
	if _, err := DecodePayload(raw); err == nil {
		t.Fatal("unknown recovery controller field was accepted")
	}
}

func TestCompileRecoveryRejectsInvalidControllerURL(t *testing.T) {
	compiler := recoveryhttp.New(recoveryhttp.Options{})
	defer func() { _ = compiler.Close() }()
	spec := &RecoverySpec{
		Controller: recovery.ControllerSpec{URL: "redis://control.example.com"},
		Triggers:   RecoveryTriggerSpec{TransportError: true},
		Budget:     RecoveryBudgetSpec{MaxRoundTrips: 2},
	}
	if _, err := CompileRecovery(spec, compiler); err == nil {
		t.Fatal("invalid recovery controller URL was accepted")
	}
}

func TestUnexpectedStatusMatcherTreatsConfiguredRangesAsExpected(t *testing.T) {
	policy := &recovery.UnexpectedStatusPolicy{Expected: []recovery.StatusRange{{From: 200, To: 299}, {From: 304, To: 304}}}
	if policy.Matches(200) || policy.Matches(304) || !policy.Matches(429) {
		t.Fatalf("unexpected status matcher result")
	}
}
