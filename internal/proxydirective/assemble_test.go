package proxydirective

import "testing"

func TestToPlan(t *testing.T) {
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target:  TargetSection{URL: "https://api.example.com/base"},
		Transport: &TransportSection{
			Proxy: "socks5://user:pass@127.0.0.1:1080",
		},
		Labels: map[string]any{
			"trace_id": "trace-123",
		},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "=", Name: "Authorization", Values: []string{"Bearer secret"}},
			{Op: "=", Name: "X-Test", Values: []string{"a"}},
		}},
	}, AssembleOptions{
		StripHeaders: []string{"Authorization"},
	})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if plan.Target.String() != "https://api.example.com/base" {
		t.Fatalf("unexpected target: %s", plan.Target.String())
	}
	if plan.Proxy == nil || plan.Proxy.String() != "socks5://user:pass@127.0.0.1:1080" {
		t.Fatalf("unexpected proxy: %#v", plan.Proxy)
	}
	if !plan.JoinPath {
		t.Fatal("expected default join path")
	}
	if len(plan.HeaderOps) != 3 {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
	if got := plan.Labels["trace_id"]; got != "trace-123" {
		t.Fatalf("unexpected directive labels: %#v", plan.Labels)
	}
}

func TestToPlanBuildsReplaceHeaderMode(t *testing.T) {
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target:  TargetSection{URL: "https://api.example.com/base"},
		Headers: &HeaderSection{
			Mode: "replace",
			Ops: []HeaderOp{
				{Op: "=", Name: "Host", Values: []string{"custom.example.com"}},
			},
		},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if plan.HeaderMode != "replace" {
		t.Fatalf("unexpected header mode: %s", plan.HeaderMode)
	}
	if len(plan.HeaderOps) != 1 || plan.HeaderOps[0].Name != "Host" {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
}

func TestToPlanAllowsJoinPathFalse(t *testing.T) {
	joinPath := false
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target: TargetSection{
			URL:      "https://api.example.com/base",
			JoinPath: &joinPath,
		},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if plan.JoinPath {
		t.Fatal("expected join path to be disabled")
	}
}

func TestToPlanLeavesCaptureDisabledWhenPayloadOmitsCapture(t *testing.T) {
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target:  TargetSection{URL: "https://api.example.com/base"},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if plan.Capture.Configured {
		t.Fatal("expected capture policy to stay disabled when payload capture is absent")
	}
}

func TestToPlanBuildsCapturePolicy(t *testing.T) {
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target:  TargetSection{URL: "https://api.example.com/base"},
		Capture: &CapturePolicy{
			Request:  []string{"body"},
			Response: []string{"headers"},
			Stream: &CaptureStreamSection{
				Events:     true,
				EventTypes: []string{"response.delta"},
			},
		},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if !plan.Capture.RequestHeaders {
		t.Fatal("expected request body recording to make request headers effective")
	}
	if !plan.Capture.ResponseHeaders {
		t.Fatal("expected response headers recording to be enabled")
	}
	if !plan.Capture.RequestBody {
		t.Fatal("expected request body recording to be enabled")
	}
	if plan.Capture.ResponseBody {
		t.Fatal("expected response body recording to be disabled")
	}
	if !plan.Capture.StreamEvents {
		t.Fatal("expected stream event recording to be enabled")
	}
	if len(plan.Capture.StreamEventTypes) != 1 || plan.Capture.StreamEventTypes[0] != "response.delta" {
		t.Fatalf("unexpected stream event types: %#v", plan.Capture.StreamEventTypes)
	}
}

func TestToPlanCaptureBodyMakesHeadersEffective(t *testing.T) {
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target:  TargetSection{URL: "https://api.example.com/base"},
		Capture: &CapturePolicy{
			Request:  []string{"body"},
			Response: []string{"body"},
		},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}

	if !plan.Capture.RequestHeaders {
		t.Fatal("expected request body recording to make request headers effective")
	}
	if !plan.Capture.ResponseHeaders {
		t.Fatal("expected response body recording to make response headers effective")
	}
}

func TestToPlanIgnoresEmptyCapturePolicy(t *testing.T) {
	plan, err := ToPlan(Payload{
		Version: PayloadVersion,
		Kind:    PayloadKind,
		Target:  TargetSection{URL: "https://api.example.com/base"},
		Capture: &CapturePolicy{},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if !plan.Capture.Configured {
		t.Fatal("expected capture policy to be configured")
	}
	if plan.Capture.RequestHeaders || plan.Capture.ResponseHeaders ||
		plan.Capture.RequestBody || plan.Capture.ResponseBody || plan.Capture.StreamEvents {
		t.Fatalf("unexpected capture outputs: %#v", plan.Capture)
	}
}
