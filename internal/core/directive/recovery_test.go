package directive

import (
	"encoding/json"
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
			Controller: RecoveryControllerSpec{
				Module: recoveryhttp.Name,
				Config: json.RawMessage(`{"url":"https://control.example.com/recovery","headers":{"authorization":"Bearer secret"}}`),
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
	if spec.Controller.Module != recoveryhttp.Name || spec.Budget.MaxElapsed != "30s" ||
		spec.Triggers.UnexpectedStatus.CaptureBodyBytes != 64<<10 {
		t.Fatalf("unexpected normalized recovery payload: %#v", spec)
	}
	definition := recoveryhttp.New(recoveryhttp.Options{})
	defer func() { _ = definition.Close() }()
	registry, err := recovery.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileRecovery(spec, registry)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Budget.MaxRoundTrips != 3 || compiled.Budget.MaxElapsed != 30*time.Second ||
		compiled.Triggers.ResponseHeaderTimeout != 10*time.Second || !compiled.Triggers.UnexpectedStatus.Matches(401) ||
		compiled.Triggers.UnexpectedStatus.Matches(204) || compiled.ControllerModule != recoveryhttp.Name {
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
			Controller: RecoveryControllerSpec{Module: recoveryhttp.Name, Config: json.RawMessage(`{"url":"https://control.example.com/recovery"}`)},
			Triggers: RecoveryTriggerSpec{UnexpectedStatus: &RecoveryUnexpectedStatusSpec{
				Expected: []RecoveryStatusRangeSpec{{From: 200, To: 299}},
			}},
			Budget: RecoveryBudgetSpec{MaxRoundTrips: 3},
		}
	}
	for name, mutate := range map[string]func(*RecoverySpec){
		"controller module": func(spec *RecoverySpec) { spec.Controller.Module = "" },
		"controller config": func(spec *RecoverySpec) { spec.Controller.Config = json.RawMessage(`{`) },
		"empty triggers":    func(spec *RecoverySpec) { spec.Triggers = RecoveryTriggerSpec{} },
		"status range":      func(spec *RecoverySpec) { spec.Triggers.UnexpectedStatus.Expected[0].From = 100 },
		"round-trip budget": func(spec *RecoverySpec) { spec.Budget.MaxRoundTrips = 0 },
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

func TestCompileRecoveryRejectsInvalidControllerConfig(t *testing.T) {
	definition := recoveryhttp.New(recoveryhttp.Options{})
	defer func() { _ = definition.Close() }()
	registry, err := recovery.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	spec := &RecoverySpec{
		Controller: RecoveryControllerSpec{Module: recoveryhttp.Name, Config: json.RawMessage(`{"url":"redis://control.example.com"}`)},
		Triggers:   RecoveryTriggerSpec{TransportError: true},
		Budget:     RecoveryBudgetSpec{MaxRoundTrips: 2},
	}
	if _, err := CompileRecovery(spec, registry); err == nil {
		t.Fatal("invalid recovery controller config was accepted")
	}
}

func TestUnexpectedStatusMatcherTreatsConfiguredRangesAsExpected(t *testing.T) {
	policy := &recovery.UnexpectedStatusPolicy{Expected: []recovery.StatusRange{{From: 200, To: 299}, {From: 304, To: 304}}}
	if policy.Matches(200) || policy.Matches(304) || !policy.Matches(429) {
		t.Fatalf("unexpected status matcher result")
	}
}
