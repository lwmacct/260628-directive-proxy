package directive

import (
	"context"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

type compilerFunc func(module.Specs) (*program.Executable, error)

func (compile compilerFunc) Compile(source module.Specs) (*program.Executable, error) {
	return compile(source)
}

type recoveryCompilerFunc func(module.Spec) (recovery.ControllerBinding, error)

func (compile recoveryCompilerFunc) Compile(spec module.Spec) (recovery.ControllerBinding, error) {
	return compile(spec)
}

type recoveryBindingFunc func(context.Context, recovery.Event) (recovery.Decision, error)

func (binding recoveryBindingFunc) Decide(ctx context.Context, event recovery.Event) (recovery.Decision, error) {
	return binding(ctx, event)
}

const testTokenSecret = "test-directive-token-secret"

func newTestResolver(opts ...ResolverOptions) *Resolver {
	var configured ResolverOptions
	if len(opts) > 0 {
		configured = opts[0]
	}
	if configured.Compiler == nil {
		configured.Compiler = compilerFunc(func(module.Specs) (*program.Executable, error) {
			return &program.Executable{}, nil
		})
	}
	if configured.RecoveryCompiler == nil {
		configured.RecoveryCompiler = recoveryCompilerFunc(func(module.Spec) (recovery.ControllerBinding, error) {
			return recoveryBindingFunc(func(context.Context, recovery.Event) (recovery.Decision, error) {
				return recovery.Decision{Action: recovery.ActionFail}, nil
			}), nil
		})
	}
	configured.TokenSecret = testTokenSecret
	return NewResolver(configured).(*Resolver)
}
