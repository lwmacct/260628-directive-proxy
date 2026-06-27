// Package proxy provides a reusable dynamic reverse proxy handler.
//
// The package does not know anything about LLM-specific request formats. Callers
// supply a proxyplan.Resolver that converts an incoming request into a
// proxyplan.Plan, and the handler applies that plan to a standard
// reverse proxy.
//
// Example:
//
//	handler := proxy.NewHandler(resolver, transport, proxy.HandlerOptions{})
//	_ = handler
package proxyhttp
