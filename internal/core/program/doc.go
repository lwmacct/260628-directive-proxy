// Package program compiles globally ordered directive module programs and
// executes their exchange and round-trip lifetimes. An Executable is immutable:
// Compile runs once for a resolved Payload, while each recovery round trip only
// opens fresh round-trip instances from the existing bindings.
package program
