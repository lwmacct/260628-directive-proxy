package program

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type runtimeDefinition struct {
	name    string
	compile func() module.Binding
}

func (definition runtimeDefinition) Name() string { return definition.name }
func (definition runtimeDefinition) Compile(module.CompileContext, json.RawMessage) (module.Binding, error) {
	return definition.compile(), nil
}

func TestRuntimeContainsModulePanicsAndDegradesDefinition(t *testing.T) {
	definition := runtimeDefinition{name: "panic.module", compile: func() module.Binding {
		return testBinding{open: func() module.Instance {
			return testInstance{bind: func(registrar module.Registrar) {
				registrar.OnRequestStarted(module.SyncPolicy(), func(module.Context, lifecycle.RequestStarted) error { panic("boom") })
			}}
		}}
	}}
	runtime, err := NewRuntime([]module.Definition{definition}, nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(Program{{Scope: module.ScopeExchange, ID: "panic", Module: "panic.module", Config: []byte(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, runtimeMetadata(t, "trace"))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := run.OpenExchange(module.OpenContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewScopeSet(scope).RequestStarted(t.Context(), lifecycle.RequestStarted{}); err == nil {
		t.Fatal("module panic did not fail the barrier")
	}
	health := runtime.ModuleHealth()
	if health.Status != "degraded" || health.Modules["panic.module"].Status != "degraded" {
		t.Fatalf("module panic did not degrade health: %#v", health)
	}
}

func TestExecutableCompilesOnceAndOpensEachAttempt(t *testing.T) {
	compileCalls := 0
	openCalls := 0
	definition := runtimeDefinition{name: "attempt.module", compile: func() module.Binding {
		compileCalls++
		return testBinding{open: func() module.Instance {
			openCalls++
			return testInstance{}
		}}
	}}
	runtime, err := NewRuntime([]module.Definition{definition}, nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(Program{{Scope: module.ScopeAttempt, ID: "attempt", Module: "attempt.module", Config: []byte(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, runtimeMetadata(t, "trace"))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		scope, openErr := run.OpenAttempt(module.OpenContext{Attempt: attempt})
		if openErr != nil {
			t.Fatal(openErr)
		}
		if finishErr := scope.Finish(t.Context(), module.FinishCompleted); finishErr != nil {
			t.Fatal(finishErr)
		}
	}
	if compileCalls != 1 || openCalls != 2 {
		t.Fatalf("unexpected compile/open counts: compile=%d open=%d", compileCalls, openCalls)
	}
}

func TestRuntimeFailsClosedAfterClose(t *testing.T) {
	runtime, err := NewRuntime(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(Program{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	if _, err := runtime.Compile(Program{}); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("closed runtime compiled a program: %v", err)
	}
	if _, err := runtime.StartRun("trace", executable, runtimeMetadata(t, "trace")); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("closed runtime started a run: %v", err)
	}
}

func runtimeMetadata(t *testing.T, traceID string) metadata.Set {
	t.Helper()
	fields, err := metadata.Compile(map[string]string{metadata.KeyUserKey: "uk_test"})
	if err != nil {
		t.Fatal(err)
	}
	fields, err = fields.WithTraceID(traceID)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}
