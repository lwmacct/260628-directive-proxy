package proxy

import (
	"context"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
)

type preparedRequestContextKey struct{}

type preparedRequest struct {
	directive *PreparedDirective
	template  *RequestTemplate
	body      *bodystore.Store
}

func contextWithPreparedRequest(ctx context.Context, directive *PreparedDirective, template *RequestTemplate, body ...*bodystore.Store) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	var requestBody *bodystore.Store
	if len(body) > 0 {
		requestBody = body[0]
	}
	return context.WithValue(ctx, preparedRequestContextKey{}, preparedRequest{directive: directive, template: template, body: requestBody})
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
