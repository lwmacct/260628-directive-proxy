package httpheader

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyRequestPreservesDirectiveOrderAfterBaselineCleanup(t *testing.T) {
	original := http.Header{
		"Authorization":   {"Bearer inbound"},
		"Connection":      {"X-Hop"},
		"Content-Type":    {"application/json"},
		"X-Forwarded-For": {"192.0.2.1"},
		"X-Hop":           {"drop"},
	}
	request := httptest.NewRequest(http.MethodPost, "https://resolver.example/", nil)
	ApplyRequest(request, original, RequestPlan{
		StripBeforeOps: []string{"Authorization"},
		Ops: []Op{
			{Action: ActionSet, Selector: Selector{Kind: SelectorExact, Pattern: "Authorization"}, Values: []string{"Bearer resolver"}},
			{Action: ActionSet, Selector: Selector{Kind: SelectorExact, Pattern: "Content-Type"}, Values: []string{"application/vnd.directive+json"}},
		},
	}, RequestOptions{})

	if request.Header.Get("Authorization") != "Bearer resolver" || request.Header.Get("Content-Type") != "application/vnd.directive+json" {
		t.Fatalf("directive operations were not applied: %#v", request.Header)
	}
	for _, name := range []string{"Connection", "X-Hop", "X-Forwarded-For"} {
		if request.Header.Get(name) != "" {
			t.Fatalf("baseline cleanup did not remove %s: %#v", name, request.Header)
		}
	}
	if values, exists := request.Header["User-Agent"]; !exists || values != nil {
		t.Fatalf("default user-agent was not suppressed for inherited baseline: exists=%t values=%#v", exists, values)
	}
}

func TestApplyRequestDeleteAllUsesOnlyDirectiveHeaders(t *testing.T) {
	original := http.Header{"Content-Type": {"application/json"}, "Cookie": {"session=trusted"}}
	request := httptest.NewRequest(http.MethodPost, "https://resolver.example/", nil)
	ApplyRequest(request, original, RequestPlan{
		Ops: []Op{
			{Action: ActionDel, Selector: Selector{Kind: SelectorGlob, Pattern: "*"}},
			{Action: ActionSet, Selector: Selector{Kind: SelectorExact, Pattern: "X-Resolver"}, Values: []string{"primary"}},
		},
	}, RequestOptions{})

	if request.Header.Get("Content-Type") != "" || request.Header.Get("Cookie") != "" || request.Header.Get("X-Resolver") != "primary" {
		t.Fatalf("delete-all mutation did not honor directive headers: %#v", request.Header)
	}
	if values, exists := request.Header["User-Agent"]; !exists || values != nil {
		t.Fatalf("default user-agent was not suppressed: exists=%t values=%#v", exists, values)
	}
}

func TestApplyRequestRemovesEveryProxyDisclosureHeader(t *testing.T) {
	original := make(http.Header)
	for _, name := range proxyDisclosureHeaders {
		original.Set(name, "remove")
	}
	request := httptest.NewRequest(http.MethodGet, "https://resolver.example/", nil)
	ApplyRequest(request, original, RequestPlan{}, RequestOptions{})
	for _, name := range proxyDisclosureHeaders {
		if request.Header.Get(name) != "" {
			t.Fatalf("proxy disclosure header %s was preserved: %#v", name, request.Header)
		}
	}
}

func TestApplyUsesOrderedGlobOperations(t *testing.T) {
	headers := make(http.Header)
	Apply(headers, []Op{
		{Action: ActionSet, Selector: Selector{Kind: SelectorGlob, Pattern: "X-*"}, Values: []string{"miss"}},
		{Action: ActionSet, Selector: Selector{Kind: SelectorExact, Pattern: "X-Created"}, Values: []string{"first"}},
		{Action: ActionSet, Selector: Selector{Kind: SelectorGlob, Pattern: "x-*"}, Values: []string{"second"}},
	})
	if headers.Get("X-Created") != "second" {
		t.Fatalf("operations did not preserve directive order: %#v", headers)
	}
}
