// Package directive defines the public wire contract shared by directive
// producers and the directive proxy data plane.
//
// It owns Payload and RemoteSpec schemas, strict JSON decoding, validation,
// normalization, and target-less Payload templates. Token signing and proxy
// runtime behavior intentionally remain private to the directive proxy.
package directive
