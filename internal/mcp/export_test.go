package mcp

import "io"

// SetDiagnosticsForTest points the diagnostics of the adapter at w, and returns the
// function that restores them. It is for the tests of this package's callers, which
// read a panic's stack from the writer.
func SetDiagnosticsForTest(w io.Writer) (restore func()) {
	prev := diagnostics
	diagnostics = w
	return func() { diagnostics = prev }
}
