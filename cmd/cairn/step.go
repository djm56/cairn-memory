package main

import (
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runStep is the sub-command `cairn step`, the steps of a task: add and update (issue #87; the
// verbs are in internal/cli). The project and the server are resolved only once the arguments
// pass the usage check.
//
// Exit codes: 0 when the step was written, 1 when the project, the server or the call failed,
// 2 for a usage error.
func runStep(args []string, stdout, stderr io.Writer) int {
	return cli.Step(openSession, args, os.Stdin, stdout, stderr)
}
