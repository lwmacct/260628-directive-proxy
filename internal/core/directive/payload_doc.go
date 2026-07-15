// Package directive provides the canonical dproxy.<version> directive token format
// used by the directive proxy data plane.
//
// The package owns:
//
//   - the payload schema
//   - the inline / remote Document model
//   - dproxy.<version> Document encoding and complete decoding
//   - payload and RemoteSpec validation
//
// Resolvers extract directive tokens from Authorization bearer headers and
// translate the decoded payload into a proxy.Plan.
package directive
