package recovery

import (
	"context"
	"testing"
	"time"
)

type controllerBindingStub struct{}

func (*controllerBindingStub) Decide(context.Context, Event) (Decision, error) {
	return Decision{Action: ActionFail}, nil
}

func TestNormalizeControllerSpecCanonicalizesDirectHTTPParameters(t *testing.T) {
	source := ControllerSpec{
		URL: " https://user:secret@control.example.com/recovery?tenant=a ",
		Headers: map[string]string{
			"authorization": "Bearer secret",
		},
	}
	normalized, err := NormalizeControllerSpec(source)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.URL != "https://user:secret@control.example.com/recovery?tenant=a" ||
		normalized.Timeout != DefaultControllerTimeout.String() || normalized.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("unexpected normalized controller: %#v", normalized)
	}
	normalized.Headers["Authorization"] = "mutated"
	if source.Headers["authorization"] != "Bearer secret" {
		t.Fatal("normalized controller retained the source header map")
	}
}

func TestNormalizeControllerSpecRejectsInvalidParameters(t *testing.T) {
	invalid := []ControllerSpec{
		{},
		{URL: "redis://control.example.com"},
		{URL: "https://control.example.com", Timeout: "0s"},
		{URL: "https://control.example.com", Timeout: "11m"},
		{URL: "https://control.example.com", Headers: map[string]string{"Bad Header": "value"}},
		{URL: "https://control.example.com", Headers: map[string]string{"X-Test": "bad\nvalue"}},
		{URL: "https://control.example.com", Headers: map[string]string{"x-test": "one", "X-Test": "two"}},
	}
	for _, spec := range invalid {
		if _, err := NormalizeControllerSpec(spec); err == nil {
			t.Fatalf("invalid controller spec was accepted: %#v", spec)
		}
	}
}

func TestClonePolicyReusesControllerBindingAndClonesTriggerPolicy(t *testing.T) {
	binding := &controllerBindingStub{}
	policy := &Policy{
		Controller: binding,
		Triggers: TriggerPolicy{UnexpectedStatus: &UnexpectedStatusPolicy{
			Expected: []StatusRange{{From: 200, To: 299}}, CaptureBodyBytes: 1024,
		}},
		Budget: Budget{MaxRoundTrips: 3, MaxElapsed: time.Minute},
	}
	first := ClonePolicy(policy)
	second := ClonePolicy(policy)
	first.Triggers.UnexpectedStatus.Expected[0].From = 201
	if first.Controller != binding || second.Controller != binding || policy.Triggers.UnexpectedStatus.Expected[0].From != 200 {
		t.Fatalf("policy clone replaced the binding or retained mutable trigger state: first=%#v second=%#v", first, second)
	}
}
