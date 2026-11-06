package main

import (
	"io"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runDirective is the sub-command `cairn directive`, the binding rules that apply to a project: list
// (issue #87; the verb is in internal/cli). The project and the server are resolved only once the
// arguments pass the usage check.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the project, the server or the call failed (cli.ExitCodeFor),
// 2 for a usage error.
func runDirective(args []string, stdout, stderr io.Writer) int {
	return cli.Directive(openSession, args, stdout, stderr)
}
