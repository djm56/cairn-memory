package main

import (
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runSearch is the sub-command `cairn search`, a full-text search of the project's rows (issue #87; the
// command is in internal/cli). The project and the server are resolved only once the arguments pass the
// usage check.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the project, the server or the call failed (cli.ExitCodeFor),
// 2 for a usage error.
func runSearch(args []string, stdout, stderr io.Writer) int {
	return cli.Search(openSession, args, os.Stdin, stdout, stderr)
}
