package proxy

import "context"

type contextKey struct{}

func ContextWithPlan(ctx context.Context, plan *Plan) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, plan)
}

func PlanFromContext(ctx context.Context) (*Plan, bool) {
	if ctx == nil {
		return nil, false
	}
	plan, ok := ctx.Value(contextKey{}).(*Plan)
	if !ok || plan == nil {
		return nil, false
	}
	return plan, true
}
