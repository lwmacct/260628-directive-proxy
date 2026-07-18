package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type controllerDefinitionStub struct {
	name    string
	compile func(json.RawMessage) (ControllerBinding, error)
}

func (definition controllerDefinitionStub) Name() string { return definition.name }
func (definition controllerDefinitionStub) Compile(config json.RawMessage) (ControllerBinding, error) {
	return definition.compile(config)
}

type controllerBindingStub struct{}

func (*controllerBindingStub) Decide(context.Context, Event) (Decision, error) {
	return Decision{Action: ActionFail}, nil
}

func TestRegistryRejectsDuplicateAndUnknownControllerModules(t *testing.T) {
	definition := controllerDefinitionStub{name: "test.controller", compile: func(json.RawMessage) (ControllerBinding, error) {
		return &controllerBindingStub{}, nil
	}}
	if _, err := NewRegistry(definition, definition); err == nil {
		t.Fatal("duplicate controller module was accepted")
	}
	registry, err := NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Compile(ControllerSpec{Module: "missing.controller"}); err == nil {
		t.Fatal("unknown controller module was accepted")
	}
}

func TestRegistryPropagatesConfigErrors(t *testing.T) {
	registry, err := NewRegistry(controllerDefinitionStub{name: "test.controller", compile: func(json.RawMessage) (ControllerBinding, error) {
		return nil, errors.New("invalid config")
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Compile(ControllerSpec{Module: "test.controller", Config: json.RawMessage(`{"bad":true}`)}); err == nil {
		t.Fatal("controller config error was ignored")
	}
}

func TestCompiledControllerBindingIsReusedByPolicyClones(t *testing.T) {
	compileCalls := 0
	binding := &controllerBindingStub{}
	registry, err := NewRegistry(controllerDefinitionStub{name: "test.controller", compile: func(config json.RawMessage) (ControllerBinding, error) {
		compileCalls++
		if string(config) != `{"value":1}` {
			t.Fatalf("unexpected controller config: %s", config)
		}
		return binding, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := registry.Compile(ControllerSpec{Module: "test.controller", Config: json.RawMessage(`{"value":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	policy := &Policy{ControllerModule: "test.controller", Controller: compiled}
	first := ClonePolicy(policy)
	second := ClonePolicy(policy)
	if compileCalls != 1 || first.Controller != binding || second.Controller != binding {
		t.Fatalf("controller binding was recompiled or replaced: calls=%d first=%p second=%p", compileCalls, first.Controller, second.Controller)
	}
}
