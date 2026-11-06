package main

import (
	"fmt"
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runTask is the sub-command `cairn task`, the reads and writes of a task (issue #87; the verbs
// are in internal/cli). The project and the server are resolved for the working directory only
// once the arguments pass the usage check, so `cairn task` with no verb prints the usage whether or
// not a project is set. No store is opened: the calls go through internal/client.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the project, the server or the call failed (cli.ExitCodeFor),
// 2 for a usage error.
func runTask(args []string, stdout, stderr io.Writer) int {
	return cli.Task(openSession, args, os.Stdin, stdout, stderr)
}

// openSession resolves the project and the server for the working directory, and returns the
// session a sub-command calls the server through. The sub-commands call it only after their
// arguments pass the usage check.
func openSession() (cli.Session, error) {
	wd, err := workDir()
	if err != nil {
		return cli.Session{}, fmt.Errorf("the working directory: %w", err)
	}
	t, err := dial(wd, 0)
	if err != nil {
		return cli.Session{}, err
	}
	return cli.Session{Svc: t.svc, Ctx: t.ctx, Project: t.project.Name}, nil
}
