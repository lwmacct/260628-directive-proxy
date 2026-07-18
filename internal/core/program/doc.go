// Package program compiles globally ordered directive module programs and
// executes their exchange and attempt scopes. An Executable is immutable:
// Compile runs once for a resolved Payload, while each recovery attempt only
// opens fresh attempt instances from the existing bindings.
package program
