package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type testReadWriteCloser struct{ bytes.Buffer }

func (*testReadWriteCloser) Close() error { return nil }

func TestModifyResponseAppliesOrderedOpsAndStripsReservedHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://upstream.example", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Connection":        {"X-Connection-Only"},
			"Server":            {"upstream"},
			"Set-Cookie":        {"a=1"},
			"X-Connection-Only": {"drop"},
			"X-Dproxy-Trace-Id": {"forged"},
			"X-Upstream-One":    {"drop"},
		},
		Request: request,
	}
	bindResponseHeaderPlan(response, request, httpheader.ResponsePlan{Ops: []httpheader.Op{
		{Action: httpheader.ActionRemove, Selector: exactSelector("Server")},
		{Action: httpheader.ActionRemove, Selector: globSelector("X-Upstream-*")},
		{Action: httpheader.ActionAdd, Selector: exactSelector("Set-Cookie"), Values: []string{"b=2"}},
		{Action: httpheader.ActionSet, Selector: exactSelector("Access-Control-Allow-Origin"), Values: []string{"*"}},
	}})

	if err := modifyResponse(response); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Connection", "Server", "X-Connection-Only", "X-Dproxy-Trace-Id", "X-Upstream-One"} {
		if got := response.Header.Get(name); got != "" {
			t.Fatalf("expected %s to be removed, got %q", name, got)
		}
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("unexpected CORS header: %q", got)
	}
	if got := response.Header.Values("Set-Cookie"); len(got) != 2 || got[0] != "a=1" || got[1] != "b=2" {
		t.Fatalf("unexpected Set-Cookie values: %#v", got)
	}
}

func TestModifyResponsePreservesSwitchingProtocolHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://upstream.example", nil)
	response := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Header: http.Header{
			"Connection": {"Upgrade"},
			"Upgrade":    {"websocket"},
			"Server":     {"upstream"},
		},
		Request: request,
	}
	bindResponseHeaderPlan(response, request, httpheader.ResponsePlan{Ops: []httpheader.Op{
		{Action: httpheader.ActionRemove, Selector: globSelector("*")},
	}})

	if err := modifyResponse(response); err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Connection") != "Upgrade" || response.Header.Get("Upgrade") != "websocket" {
		t.Fatalf("upgrade headers changed: %#v", response.Header)
	}
	if response.Header.Get("Server") != "" {
		t.Fatalf("ordinary response header was not removed: %#v", response.Header)
	}
}

func TestSwitchingProtocolCancelWrapperRemainsWritable(t *testing.T) {
	body := &testReadWriteCloser{}
	response := &http.Response{StatusCode: http.StatusSwitchingProtocols, Body: body}
	wrapped := wrapCancelOnCloseBody(response, func() {})
	readWriteCloser, ok := wrapped.(io.ReadWriteCloser)
	if !ok {
		t.Fatalf("switching protocol body lost io.ReadWriteCloser: %T", wrapped)
	}
	if _, err := readWriteCloser.Write([]byte("websocket")); err != nil || body.String() != "websocket" {
		t.Fatalf("write was not forwarded: body=%q err=%v", body.String(), err)
	}
}

func TestHandlerAppliesResponseHeaderPlan(t *testing.T) {
	target, _ := url.Parse("https://upstream.example")
	handler := NewHandler(
		resolverFunc(func(*http.Request) (Resolution, error) {
			return Resolution{Plan: &Plan{
				Target: target,
				Headers: httpheader.Plan{Response: httpheader.ResponsePlan{Ops: []httpheader.Op{
					{Action: httpheader.ActionRemove, Selector: exactSelector("Server")},
					{Action: httpheader.ActionSet, Selector: exactSelector("X-Downstream"), Values: []string{"rewritten"}},
				}}},
			}}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Server": {"upstream"}},
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    request,
			}, nil
		}),
		HandlerOptions{},
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://proxy.local/test", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("unexpected response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Server") != "" || recorder.Header().Get("X-Downstream") != "rewritten" {
		t.Fatalf("unexpected downstream headers: %#v", recorder.Header())
	}
}
