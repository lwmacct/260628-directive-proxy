package directive

import "testing"

func TestToPlan(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Proxy:  "socks5://user:pass@127.0.0.1:1080",
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
		Target: TargetSection{URL: "https://api.example.com/base"},
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
	if len(plan.HeaderOps) != 1 || plan.HeaderOps[0].Selector.Pattern != "Host" {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
}

func TestToPlanBuildsGlobHeaderSelector(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "-", Glob: "M-Runtime-*"},
		}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if len(plan.HeaderOps) != 1 || plan.HeaderOps[0].Selector.Kind != "glob" || plan.HeaderOps[0].Selector.Pattern != "M-Runtime-*" {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
}

func TestToPlanBuildsProxyDisclosurePresetSelector(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "-", Preset: "proxy-disclosure"},
		}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if len(plan.HeaderOps) != 1 || plan.HeaderOps[0].Selector.Kind != "preset" || plan.HeaderOps[0].Selector.Pattern != "proxy-disclosure" {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
}

func TestToPlanAllowsJoinPathFalse(t *testing.T) {
	joinPath := false
	plan, err := ToPlan(Payload{
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

func TestToPlanExtractsDproxyMetadataAndRemovesItFromOutboundOps(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "=", Name: "x-dproxy-request-id", Values: []string{"request-1"}},
			{Op: "+", Name: "X-Dproxy-Request-ID", Values: []string{"request-2"}},
			{Op: "=", Name: "X-Upstream", Values: []string{"forwarded"}},
		}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Metadata["X-Dproxy-Request-Id"]; len(got) != 2 || got[0] != "request-1" || got[1] != "request-2" {
		t.Fatalf("unexpected metadata: %#v", plan.Metadata)
	}
	if len(plan.HeaderOps) != 1 || plan.HeaderOps[0].Selector.Pattern != "X-Upstream" {
		t.Fatalf("metadata leaked into outbound ops: %#v", plan.HeaderOps)
	}
}

func TestToPlanRejectsReservedOrInvalidDproxyMetadata(t *testing.T) {
	for _, op := range []HeaderOp{
		{Op: "=", Name: "X-Dproxy-Trace-ID", Values: []string{"forged"}},
		{Op: "=", Name: "X-Dproxy-Request-ID", Values: []string{""}},
		{Op: "=", Name: "X-Dproxy-Request-ID", Values: []string{" padded "}},
		{Op: "=", Name: "X-Dproxy-Request-ID", Values: []string{"bad\nvalue"}},
		{Op: "=", Name: "Dproxy-Retry-ID", Values: []string{"forged"}},
	} {
		if _, err := ToPlan(Payload{Target: TargetSection{URL: "https://api.example.com"}, Headers: &HeaderSection{Ops: []HeaderOp{op}}}, AssembleOptions{}); err == nil {
			t.Fatalf("expected invalid metadata op: %#v", op)
		}
	}
}
