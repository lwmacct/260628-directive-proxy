package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type controllerDefinitionStub struct {
	name    string
	compile func(json.RawMessage) (ControllerBinding, error)
}

func (definition controllerDefinitionStub) Name() string { return definition.name }
func (definition controllerDefinitionStub) CompileController(config json.RawMessage) (ControllerBinding, error) {
	return definition.compile(config)
}

type controllerBindingStub struct{}

func (*controllerBindingStub) Decide(context.Context, Event) (Decision, error) {
	return Decision{Action: ActionFail}, nil
}

func TestCatalogRejectsDuplicateModuleNamesAndCompilerRejectsUnknownModules(t *testing.T) {
	definition := controllerDefinitionStub{name: "test.controller", compile: func(json.RawMessage) (ControllerBinding, error) {
		return &controllerBindingStub{}, nil
	}}
	if _, err := module.NewCatalog(definition, programDefinitionStub{name: "test.controller"}); err == nil {
		t.Fatal("duplicate module name was accepted")
	}
	catalog := module.MustCatalog(definition)
	compiler, err := NewControllerCompiler(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(module.Spec{Module: "missing.controller"}); err == nil {
		t.Fatal("unknown controller module was accepted")
	}
}

func TestControllerCompilerRejectsModulesWithoutControllerCapability(t *testing.T) {
	catalog := module.MustCatalog(programDefinitionStub{name: "test.program"})
	compiler, err := NewControllerCompiler(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(module.Spec{Module: "test.program"}); err == nil {
		t.Fatal("program-only module was accepted as a controller")
	}
}

func TestControllerCompilerPropagatesConfigErrors(t *testing.T) {
	catalog := module.MustCatalog(controllerDefinitionStub{name: "test.controller", compile: func(json.RawMessage) (ControllerBinding, error) {
		return nil, errors.New("invalid config")
	}})
	compiler, err := NewControllerCompiler(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(module.Spec{Module: "test.controller", Config: json.RawMessage(`{"bad":true}`)}); err == nil {
		t.Fatal("controller config error was ignored")
	}
}

func TestCompiledControllerBindingIsReusedByPolicyClones(t *testing.T) {
	compileCalls := 0
	binding := &controllerBindingStub{}
	catalog := module.MustCatalog(controllerDefinitionStub{name: "test.controller", compile: func(config json.RawMessage) (ControllerBinding, error) {
		compileCalls++
		if string(config) != `{"value":1}` {
			t.Fatalf("unexpected controller config: %s", config)
		}
		return binding, nil
	}})
	compiler, err := NewControllerCompiler(catalog)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(module.Spec{Module: "test.controller", Config: json.RawMessage(`{"value":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	policy := &Policy{Controller: CompiledController{Spec: module.Spec{Module: "test.controller"}, Binding: compiled}}
	first := ClonePolicy(policy)
	second := ClonePolicy(policy)
	if compileCalls != 1 || first.Controller.Binding != binding || second.Controller.Binding != binding {
		t.Fatalf("controller binding was recompiled or replaced: calls=%d first=%p second=%p", compileCalls, first.Controller.Binding, second.Controller.Binding)
	}
}

type programDefinitionStub struct{ name string }

func (definition programDefinitionStub) Name() string   { return definition.name }
func (programDefinitionStub) Lifetime() module.Lifetime { return module.LifetimeExchange }
func (programDefinitionStub) CompileProgram(json.RawMessage) (module.Binding, error) {
	return nil, nil
}
