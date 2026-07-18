package directive

import (
	"net/url"
	"testing"
)

func TestCompilePayload(t *testing.T) {
	plan, _, err := CompilePayload(Payload{
		Target: TargetSection{BaseURL: "https://api.example.com/base"},
		Proxy:  "socks5://user:pass@127.0.0.1:1080",
		Headers: requestHeaders(
			HeaderMutation{Action: HeaderActionSet, Name: "Authorization", Values: []string{"Bearer secret"}},
			HeaderMutation{Action: HeaderActionSet, Name: "X-Test", Values: []string{"a"}},
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
	if len(plan.Headers.Request.StripBeforeOps) != 1 || len(plan.Headers.Request.Ops) != 2 {
		t.Fatalf("unexpected request header plan: %#v", plan.Headers.Request)
	}
}

func TestCompilePayloadBuildsReplaceHeaderMode(t *testing.T) {
	plan, _, err := CompilePayload(Payload{
		Target: TargetSection{BaseURL: "https://api.example.com/base"},
		Headers: &HeaderPolicy{Mode: "replace", Mutations: []HeaderMutation{{
			Side: HeaderSideRequest, Action: HeaderActionSet, Name: "Host", Values: []string{"custom.example.com"},
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

func TestCompilePayloadBuildsGlobHeaderSelector(t *testing.T) {
	plan, _, err := CompilePayload(Payload{
		Target: TargetSection{BaseURL: "https://api.example.com/base"},
		Headers: requestHeaders(
			HeaderMutation{Action: HeaderActionRemove, Glob: "M-Runtime-*"},
		),
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Selector.Kind != "glob" || plan.Headers.Request.Ops[0].Selector.Pattern != "M-Runtime-*" {
		t.Fatalf("unexpected header ops: %#v", plan.Headers.Request.Ops)
	}
}

func TestCompilePayloadBuildsHeaderPoliciesAndResponseOps(t *testing.T) {
	plan, _, err := CompilePayload(Payload{
		Target: TargetSection{BaseURL: "https://api.example.com/base"},
		Headers: &HeaderPolicy{PreserveProxyDisclosure: true, Mutations: []HeaderMutation{{
			Side: HeaderSideResponse, Action: HeaderActionRemove, Name: "Server",
		}}},
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	if !plan.Headers.Request.PreserveProxyDisclosure || len(plan.Headers.Response.Ops) != 1 || plan.Headers.Response.Ops[0].Selector.Pattern != "Server" {
		t.Fatalf("unexpected header plan: %#v", plan.Headers)
	}
}

func TestCompilePayloadSplitsMixedHeaderSides(t *testing.T) {
	plan, _, err := CompilePayload(Payload{
		Target: TargetSection{BaseURL: "https://api.example.com/base"},
		Headers: &HeaderPolicy{Mutations: []HeaderMutation{
			{Side: HeaderSideResponse, Action: HeaderActionRemove, Name: "Server"},
			{Side: HeaderSideRequest, Action: HeaderActionSet, Name: "X-Request", Values: []string{"request"}},
			{Side: HeaderSideResponse, Action: HeaderActionSet, Name: "X-Response", Values: []string{"response"}},
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

func TestCompilePayloadCompilesTargetAgainstInboundURL(t *testing.T) {
	tests := []struct {
		name    string
		target  TargetSection
		inbound string
		want    string
	}{
		{
			name:    "base URL joins path and query",
			target:  TargetSection{BaseURL: "https://example.com/base?source=proxy"},
			inbound: "http://proxy.local/v1/resources?client=1",
			want:    "https://example.com/base/v1/resources?source=proxy&client=1",
		},
		{
			name:    "exact URL ignores inbound URL",
			target:  TargetSection{ExactURL: "https://example.com/action?signature=fixed"},
			inbound: "http://proxy.local/v1/resources?client=1",
			want:    "https://example.com/action?signature=fixed",
		},
		{
			name:    "base URL preserves escaped path segments",
			target:  TargetSection{BaseURL: "https://example.com/base%2Ftenant"},
			inbound: "http://proxy.local/v1/a%2Fb",
			want:    "https://example.com/base%2Ftenant/v1/a%2Fb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound, err := url.Parse(tt.inbound)
			if err != nil {
				t.Fatal(err)
			}
			plan, _, err := CompilePayload(Payload{Target: tt.target}, AssembleOptions{InboundURL: inbound})
			if err != nil {
				t.Fatalf("assemble failed: %v", err)
			}
			if got := plan.Target.String(); got != tt.want {
				t.Fatalf("unexpected target: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestCompilePayloadRejectsInvalidTargetUnion(t *testing.T) {
	for _, target := range []TargetSection{
		{},
		{BaseURL: "https://api.example.com", ExactURL: "https://api.example.com/action"},
		{BaseURL: "ftp://api.example.com"},
		{ExactURL: "api.example.com/action"},
	} {
		if _, _, err := CompilePayload(Payload{Target: target}, AssembleOptions{}); err == nil {
			t.Fatalf("expected invalid target: %#v", target)
		}
	}
}

func TestCompilePayloadExtractsDproxyMetadataAndRemovesItFromOutboundOps(t *testing.T) {
	plan, _, err := CompilePayload(Payload{
		Target: TargetSection{BaseURL: "https://api.example.com"},
		Headers: requestHeaders(
			HeaderMutation{Action: HeaderActionSet, Name: "x-dproxy-request-id", Values: []string{"request-1"}},
			HeaderMutation{Action: HeaderActionAppend, Name: "X-Dproxy-Request-ID", Values: []string{"request-2"}},
			HeaderMutation{Action: HeaderActionSet, Name: "X-Upstream", Values: []string{"forwarded"}},
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

func TestCompilePayloadRejectsReservedOrInvalidDproxyMetadata(t *testing.T) {
	for _, mutation := range []HeaderMutation{
		{Action: HeaderActionSet, Name: "X-Dproxy-Trace-ID", Values: []string{"forged"}},
		{Action: HeaderActionSet, Name: "X-Dproxy-Request-ID", Values: []string{""}},
		{Action: HeaderActionSet, Name: "X-Dproxy-Request-ID", Values: []string{" padded "}},
		{Action: HeaderActionSet, Name: "X-Dproxy-Request-ID", Values: []string{"bad\nvalue"}},
	} {
		if _, _, err := CompilePayload(Payload{Target: TargetSection{BaseURL: "https://api.example.com"}, Headers: requestHeaders(mutation)}, AssembleOptions{}); err == nil {
			t.Fatalf("expected invalid metadata mutation: %#v", mutation)
		}
	}
}

func requestHeaders(mutations ...HeaderMutation) *HeaderPolicy {
	for index := range mutations {
		mutations[index].Side = HeaderSideRequest
	}
	return &HeaderPolicy{Mutations: mutations}
}
