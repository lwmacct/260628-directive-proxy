package directive

import (
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

func TestRecoveryDocumentRoundTripAndCompile(t *testing.T) {
	document := Document{
		Kind: KindRemote,
		Remote: &RemoteDocument{Source: RemoteSpec{
			Type: RemoteTypeHTTP, URL: "https://policy.example.com/resolve", Key: "routing",
		}},
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
	token, err := EncodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Recovery == nil || decoded.Recovery.Controller.Headers["Authorization"] != "Bearer secret" ||
		decoded.Recovery.Controller.Timeout != "3s" || decoded.Recovery.Budget.MaxElapsed != "30s" ||
		decoded.Recovery.Triggers.UnexpectedStatus.CaptureBodyBytes != 64<<10 {
		t.Fatalf("unexpected normalized recovery document: %#v", decoded.Recovery)
	}
	compiled, err := CompileRecovery(decoded.Recovery)
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
			if _, err := ValidateDocument(Document{
				Kind: KindInline, Payload: &Payload{Target: TargetSection{URL: "https://api.example.com"}}, Recovery: spec,
			}); err == nil {
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
