package main

import (
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runEvent is the sub-command `cairn event`, the journal of a task: log (issue #87; the verb is in
// internal/cli). The project and the server are resolved only once the arguments pass the usage
// check.
//
// Exit codes: 0 when the event was written, the code of the error kind when the project, the server or the call failed (cli.ExitCodeFor),
// 2 for a usage error.
func runEvent(args []string, stdout, stderr io.Writer) int {
	return cli.Event(openSession, args, os.Stdin, stdout, stderr)
}
