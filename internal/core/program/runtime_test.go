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
	name     string
	lifetime module.Lifetime
	compile  func() module.Binding
}

func (definition runtimeDefinition) Name() string              { return definition.name }
func (definition runtimeDefinition) Lifetime() module.Lifetime { return definition.lifetime }
func (definition runtimeDefinition) Compile(json.RawMessage) (module.Binding, error) {
	return definition.compile(), nil
}

func TestRuntimeContainsModulePanicsAndDegradesDefinition(t *testing.T) {
	definition := runtimeDefinition{name: "panic.module", lifetime: module.LifetimeExchange, compile: func() module.Binding {
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
	executable, err := runtime.Compile(Program{{Module: "panic.module", Config: []byte(`{}`)}})
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

func TestExecutableCompilesOnceAndOpensEachRoundTrip(t *testing.T) {
	compileCalls := 0
	openCalls := 0
	definition := runtimeDefinition{name: "round_trip.module", lifetime: module.LifetimeRoundTrip, compile: func() module.Binding {
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
	executable, err := runtime.Compile(Program{{Module: "round_trip.module", Config: []byte(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, runtimeMetadata(t, "trace"))
	if err != nil {
		t.Fatal(err)
	}
	for roundTrip := 1; roundTrip <= 2; roundTrip++ {
		scope, openErr := run.OpenRoundTrip(module.OpenContext{RoundTrip: roundTrip})
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

func TestRuntimeRejectsDuplicateModulesAndInvalidDefinitionLifetime(t *testing.T) {
	definition := runtimeDefinition{name: "test.module", lifetime: module.LifetimeExchange, compile: func() module.Binding {
		return testBinding{open: func() module.Instance { return testInstance{} }}
	}}
	runtime, err := NewRuntime([]module.Definition{definition}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Compile(Program{{Module: "test.module"}, {Module: "test.module"}}); err == nil {
		t.Fatal("duplicate module was accepted")
	}

	invalid, err := NewRuntime([]module.Definition{runtimeDefinition{
		name: "invalid.module", lifetime: module.Lifetime("request"),
		compile: func() module.Binding { return testBinding{open: func() module.Instance { return testInstance{} }} },
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.Compile(Program{{Module: "invalid.module"}}); err == nil {
		t.Fatal("invalid definition lifetime was accepted")
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
