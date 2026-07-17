package directive

import "testing"

func TestToPlan(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Proxy:  "socks5://user:pass@127.0.0.1:1080",
		Headers: requestHeaders(
			HeaderOp{Op: HeaderOperationSet, Name: "Authorization", Values: []string{"Bearer secret"}},
			HeaderOp{Op: HeaderOperationSet, Name: "X-Test", Values: []string{"a"}},
		),
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
	if len(plan.Headers.Request.StripBeforeOps) != 1 || len(plan.Headers.Request.Ops) != 2 {
		t.Fatalf("unexpected request header plan: %#v", plan.Headers.Request)
	}
}

func TestToPlanBuildsReplaceHeaderMode(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Headers: &HeaderPolicy{Mode: "replace", Ops: []HeaderOp{{
			Side: HeaderSideRequest, Op: HeaderOperationSet, Name: "Host", Values: []string{"custom.example.com"},
		}}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if plan.Headers.Request.Mode != "replace" {
		t.Fatalf("unexpected header mode: %s", plan.Headers.Request.Mode)
	}
	if len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Selector.Pattern != "Host" {
		t.Fatalf("unexpected header ops: %#v", plan.Headers.Request.Ops)
	}
}

func TestToPlanBuildsGlobHeaderSelector(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Headers: requestHeaders(
			HeaderOp{Op: HeaderOperationDelete, Glob: "M-Runtime-*"},
		),
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Selector.Kind != "glob" || plan.Headers.Request.Ops[0].Selector.Pattern != "M-Runtime-*" {
		t.Fatalf("unexpected header ops: %#v", plan.Headers.Request.Ops)
	}
}

func TestToPlanBuildsHeaderPoliciesAndResponseOps(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Headers: &HeaderPolicy{PreserveProxyDisclosure: true, Ops: []HeaderOp{{
			Side: HeaderSideResponse, Op: HeaderOperationDelete, Name: "Server",
		}}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if !plan.Headers.Request.PreserveProxyDisclosure || len(plan.Headers.Response.Ops) != 1 || plan.Headers.Response.Ops[0].Selector.Pattern != "Server" {
		t.Fatalf("unexpected header plan: %#v", plan.Headers)
	}
}

func TestToPlanSplitsMixedHeaderSides(t *testing.T) {
	plan, err := ToPlan(Payload{
		Target: TargetSection{URL: "https://api.example.com/base"},
		Headers: &HeaderPolicy{Ops: []HeaderOp{
			{Side: HeaderSideResponse, Op: HeaderOperationDelete, Name: "Server"},
			{Side: HeaderSideRequest, Op: HeaderOperationSet, Name: "X-Request", Values: []string{"request"}},
			{Side: HeaderSideResponse, Op: HeaderOperationSet, Name: "X-Response", Values: []string{"response"}},
		}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Selector.Pattern != "X-Request" {
		t.Fatalf("unexpected request ops: %#v", plan.Headers.Request.Ops)
	}
	if len(plan.Headers.Response.Ops) != 2 || plan.Headers.Response.Ops[0].Selector.Pattern != "Server" || plan.Headers.Response.Ops[1].Selector.Pattern != "X-Response" {
		t.Fatalf("unexpected response ops: %#v", plan.Headers.Response.Ops)
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
		Headers: requestHeaders(
			HeaderOp{Op: HeaderOperationSet, Name: "x-dproxy-request-id", Values: []string{"request-1"}},
			HeaderOp{Op: HeaderOperationAdd, Name: "X-Dproxy-Request-ID", Values: []string{"request-2"}},
			HeaderOp{Op: HeaderOperationSet, Name: "X-Upstream", Values: []string{"forwarded"}},
		),
	}, AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Metadata["X-Dproxy-Request-Id"]; len(got) != 2 || got[0] != "request-1" || got[1] != "request-2" {
		t.Fatalf("unexpected metadata: %#v", plan.Metadata)
	}
	if len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Selector.Pattern != "X-Upstream" {
		t.Fatalf("metadata leaked into outbound ops: %#v", plan.Headers.Request.Ops)
	}
}

func TestToPlanRejectsReservedOrInvalidDproxyMetadata(t *testing.T) {
	for _, op := range []HeaderOp{
		{Op: HeaderOperationSet, Name: "X-Dproxy-Trace-ID", Values: []string{"forged"}},
		{Op: HeaderOperationSet, Name: "X-Dproxy-Request-ID", Values: []string{""}},
		{Op: HeaderOperationSet, Name: "X-Dproxy-Request-ID", Values: []string{" padded "}},
		{Op: HeaderOperationSet, Name: "X-Dproxy-Request-ID", Values: []string{"bad\nvalue"}},
	} {
		if _, err := ToPlan(Payload{Target: TargetSection{URL: "https://api.example.com"}, Headers: requestHeaders(op)}, AssembleOptions{}); err == nil {
			t.Fatalf("expected invalid metadata op: %#v", op)
		}
	}
}

func requestHeaders(ops ...HeaderOp) *HeaderPolicy {
	for index := range ops {
		ops[index].Side = HeaderSideRequest
	}
	return &HeaderPolicy{Ops: ops}
}
