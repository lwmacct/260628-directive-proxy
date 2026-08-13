// Package directive implements the canonical dp.<version> token and execution
// pipeline used by the directive proxy data plane.
//
// The package owns:
//
//   - the inline / remote Document model
//   - dp.<version> Document encoding and complete decoding
//   - payload compilation into the proxy runtime
//
// The public wire schema and validation contract live in pkg/directive. Token
// signing, resolver adapters, and runtime compilation remain internal here.
package directive
