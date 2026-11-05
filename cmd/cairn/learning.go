package main

import (
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runLearning is the sub-command `cairn learning`, the proposals of a project: add and list (issue
// #87; the verbs are in internal/cli). The project and the server are resolved only once the
// arguments pass the usage check.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the project, the server or the call failed (cli.ExitCodeFor),
// 2 for a usage error.
func runLearning(args []string, stdout, stderr io.Writer) int {
	return cli.Learning(openSession, args, os.Stdin, stdout, stderr)
}
