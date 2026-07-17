package directive

const testTokenSecret = "test-directive-token-secret"

func newTestResolver(opts ...ResolverOptions) *Resolver {
	var configured ResolverOptions
	if len(opts) > 0 {
		configured = opts[0]
	}
	configured.TokenSecret = testTokenSecret
	return NewResolver(configured).(*Resolver)
}
