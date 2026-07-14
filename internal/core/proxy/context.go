package proxy

import "context"

type preparedRequestContextKey struct{}

type preparedRequest struct {
	directive PreparedDirective
	template  *RequestTemplate
}

func contextWithPreparedRequest(ctx context.Context, directive PreparedDirective, template *RequestTemplate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, preparedRequestContextKey{}, preparedRequest{directive: directive, template: template})
}

func preparedRequestFromContext(ctx context.Context) (preparedRequest, bool) {
	if ctx == nil {
		return preparedRequest{}, false
	}
	prepared, ok := ctx.Value(preparedRequestContextKey{}).(preparedRequest)
	if !ok || prepared.directive == nil || prepared.template == nil {
		return preparedRequest{}, false
	}
	return prepared, true
}
