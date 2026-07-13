// Package proxy provides a reusable dynamic reverse proxy handler.
//
// The package is independent of application-level request formats. Callers
// supply a Resolver that converts an incoming request into a
// Plan, and the handler applies that plan to a standard
// reverse proxy.
//
// Example:
//
//	handler := proxy.NewHandler(resolver, transport, proxy.HandlerOptions{})
//	_ = handler
package proxy
