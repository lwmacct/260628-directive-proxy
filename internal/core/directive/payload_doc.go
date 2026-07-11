// Package directive provides the canonical dproxy.11 directive token format
// used by the directive proxy data plane.
//
// The package owns:
//
//   - the payload schema
//   - dproxy.11 token encoding / decoding
//   - payload validation
//   - payload-level validation
//
// Resolvers extract directive tokens from Authorization bearer headers and
// translate the decoded payload into a proxy.Plan.
package directive
