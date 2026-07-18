package directive

import "github.com/lwmacct/260628-directive-proxy/internal/core/program"

type compilerFunc func(program.Program) (*program.Executable, error)

func (compile compilerFunc) Compile(source program.Program) (*program.Executable, error) {
	return compile(source)
}

const testTokenSecret = "test-directive-token-secret"

func newTestResolver(opts ...ResolverOptions) *Resolver {
	var configured ResolverOptions
	if len(opts) > 0 {
		configured = opts[0]
	}
	if configured.Compiler == nil {
		configured.Compiler = compilerFunc(func(program.Program) (*program.Executable, error) {
			return &program.Executable{}, nil
		})
	}
	configured.TokenSecret = testTokenSecret
	return NewResolver(configured).(*Resolver)
}
