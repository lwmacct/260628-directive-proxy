// Package program compiles ordered directive module programs and executes their
// request and attempt scopes. An Executable is immutable: Compile runs once for
// a resolved Payload, while each recovery attempt only opens fresh instances
// from the existing bindings.
package program
