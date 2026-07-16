package module

import (
	"encoding/json"
	"testing"
)

type runtimeDefinition struct {
	name string
	open func() Instance
}

func (definition runtimeDefinition) Name() string { return definition.name }
func (definition runtimeDefinition) Compile(json.RawMessage) (Binding, error) {
	return runtimeBinding{open: definition.open}, nil
}

type runtimeBinding struct{ open func() Instance }

func (runtimeBinding) Lifetime() Lifetime { return LifetimeRequest }
func (binding runtimeBinding) Open(OpenContext) (Instance, error) {
	return binding.open(), nil
}

func TestRuntimeContainsModulePanicsAndDegradesDefinition(t *testing.T) {
	definition := runtimeDefinition{name: "panic.module", open: func() Instance {
		return testInstance{mount: func(binder *Binder) {
			binder.OnRequestStarted(SyncPolicy(), func(EventContext, RequestStarted) error { panic("boom") })
		}}
	}}
	runtime, err := NewRuntime([]Definition{definition}, nil)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := runtime.Compile(LifetimeRequest, []Spec{{ID: "panic", Module: "panic.module", Config: []byte(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	run := runtime.StartRun("trace")
	scope, err := run.OpenScope(OpenContext{}, compiled)
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.RequestStarted(t.Context(), RequestStarted{}); err == nil {
		t.Fatal("module panic did not fail the barrier")
	}
	health := runtime.ModuleHealth()
	if health.Status != "degraded" || health.Modules["panic.module"].Status != "degraded" {
		t.Fatalf("module panic did not degrade health: %#v", health)
	}
}
