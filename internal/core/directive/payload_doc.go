// Package directive provides the canonical dproxy.12 directive token format
// used by the directive proxy data plane.
//
// The package owns:
//
//   - the payload schema
//   - dproxy.12 inline and Redis token encoding / decoding
//   - payload validation
//   - payload-level validation
//
// Resolvers extract directive tokens from Authorization bearer headers and
// translate the decoded payload into a proxy.Plan.
package directive
