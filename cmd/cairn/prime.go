package main

import (
	"io"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runPrime is the sub-command `cairn prime`: the session-start view for an agent with only a shell and
// no hook, the same block the hook prints and the active task (the command is in internal/cli). The
// project and the server are resolved only once the arguments pass the usage check.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the project, the server or the call failed (cli.ExitCodeFor), 2 for a
// usage error.
func runPrime(args []string, stdout, stderr io.Writer) int {
	return cli.Prime(openSession, args, stdout, stderr)
}
