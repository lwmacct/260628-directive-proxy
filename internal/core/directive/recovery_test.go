package directive

import (
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

func TestRecoveryPayloadRoundTripAndCompile(t *testing.T) {
	payload := Payload{
		Target: TargetSection{URL: "https://api.example.com"},
		Recovery: &RecoverySpec{
			Controller: RecoveryControllerSpec{
				URL: "https://control.example.com/recovery", Headers: map[string]string{"authorization": "Bearer secret"},
			},
			Triggers: RecoveryTriggerSpec{
				ResponseHeaderTimeout: "10s",
				UnexpectedStatus:      &RecoveryUnexpectedStatusSpec{Expected: []RecoveryStatusRangeSpec{{From: 200, To: 299}}},
				TransportError:        true,
			},
			Budget: RecoveryBudgetSpec{MaxAttempts: 3},
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
	if spec.Controller.Headers["Authorization"] != "Bearer secret" || spec.Controller.Timeout != "3s" ||
		spec.Budget.MaxElapsed != "30s" || spec.Triggers.UnexpectedStatus.CaptureBodyBytes != 64<<10 {
		t.Fatalf("unexpected normalized recovery payload: %#v", spec)
	}
	compiled, err := CompileRecovery(spec)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Budget.MaxAttempts != 3 || compiled.Budget.MaxElapsed != 30*time.Second ||
		compiled.Triggers.ResponseHeaderTimeout != 10*time.Second || !compiled.Triggers.UnexpectedStatus.Matches(401) ||
		compiled.Triggers.UnexpectedStatus.Matches(204) || compiled.Controller.Headers.Get("Authorization") != "Bearer secret" {
		t.Fatalf("unexpected compiled recovery policy: %#v", compiled)
	}
}

func TestRecoveryValidationRejectsInvalidPolicies(t *testing.T) {
	valid := func() *RecoverySpec {
		return &RecoverySpec{
			Controller: RecoveryControllerSpec{URL: "https://control.example.com/recovery"},
			Triggers: RecoveryTriggerSpec{UnexpectedStatus: &RecoveryUnexpectedStatusSpec{
				Expected: []RecoveryStatusRangeSpec{{From: 200, To: 299}},
			}},
			Budget: RecoveryBudgetSpec{MaxAttempts: 3},
		}
	}
	for name, mutate := range map[string]func(*RecoverySpec){
		"controller URL":   func(spec *RecoverySpec) { spec.Controller.URL = "redis://control.example.com" },
		"callback timeout": func(spec *RecoverySpec) { spec.Controller.Timeout = "0s" },
		"empty triggers":   func(spec *RecoverySpec) { spec.Triggers = RecoveryTriggerSpec{} },
		"status range":     func(spec *RecoverySpec) { spec.Triggers.UnexpectedStatus.Expected[0].From = 100 },
		"attempt budget":   func(spec *RecoverySpec) { spec.Budget.MaxAttempts = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			spec := valid()
			mutate(spec)
			if err := Validate(Payload{Target: TargetSection{URL: "https://api.example.com"}, Recovery: spec}); err == nil {
				t.Fatal("invalid recovery policy was accepted")
			}
		})
	}
}

func TestUnexpectedStatusMatcherTreatsConfiguredRangesAsExpected(t *testing.T) {
	policy := &recovery.UnexpectedStatusPolicy{Expected: []recovery.StatusRange{{From: 200, To: 299}, {From: 304, To: 304}}}
	if policy.Matches(200) || policy.Matches(304) || !policy.Matches(429) {
		t.Fatalf("unexpected status matcher result")
	}
}
