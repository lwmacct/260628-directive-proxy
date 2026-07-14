package proxyrequest

import "context"

type sessionContextKey struct{}

func ContextWithSession(ctx context.Context, session Session) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, session)
}

func SessionFromContext(ctx context.Context) (Session, bool) {
	if ctx == nil {
		return nil, false
	}
	session, ok := ctx.Value(sessionContextKey{}).(Session)
	return session, ok && session != nil
}
