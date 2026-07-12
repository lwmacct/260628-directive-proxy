// Package directive provides the canonical dproxy.13 directive token format
// used by the directive proxy data plane.
//
// The package owns:
//
//   - the payload schema
//   - dproxy.13 inline and remote token encoding / decoding
//   - payload validation
//   - payload-level validation
//
// Resolvers extract directive tokens from Authorization bearer headers and
// translate the decoded payload into a proxy.Plan.
package directive
