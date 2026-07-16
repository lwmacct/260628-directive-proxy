package exchange

import "context"

type contextKey struct{}

func ContextWithExchange(ctx context.Context, current *Exchange) context.Context {
	if current == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, current)
}

func FromContext(ctx context.Context) (*Exchange, bool) {
	if ctx == nil {
		return nil, false
	}
	current, ok := ctx.Value(contextKey{}).(*Exchange)
	return current, ok && current != nil
}
