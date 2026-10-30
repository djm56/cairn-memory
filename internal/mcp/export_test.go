package mcp

import "io"

// SetDiagnosticsForTest points the diagnostics of the adapter at w, and returns the
// function that restores them. It is for the external tests of this package, in
// package mcp_test, which read a panic's stack from the writer (step8_test.go). This is
// a _test.go file, so the function exists only in this package's test builds; a caller
// in another package cannot reach it (step 12 review S7).
func SetDiagnosticsForTest(w io.Writer) (restore func()) {
	prev := diagnostics
	diagnostics = w
	return func() { diagnostics = prev }
}
