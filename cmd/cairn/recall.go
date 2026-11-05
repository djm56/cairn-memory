package main

import (
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runRecall is the sub-command `cairn recall`, what applies to a goal: the directives of the project and
// the role, and the lessons, mistakes and tasks that match it (issue #87; the command is in
// internal/cli). The project and the server are resolved only once the arguments pass the usage check.
//
// Exit codes: 0 when the answer was printed, 1 when the project, the server or the call failed,
// 2 for a usage error.
func runRecall(args []string, stdout, stderr io.Writer) int {
	return cli.Recall(openSession, args, os.Stdin, stdout, stderr)
}
