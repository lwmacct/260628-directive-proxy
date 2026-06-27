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
