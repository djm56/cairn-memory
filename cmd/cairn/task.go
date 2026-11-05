package main

import (
	"fmt"
	"io"
	"os"

	"github.com/djm56/cairn-memory/internal/cli"
)

// runTask is the sub-command `cairn task`, the reads of a task (issue #87; the verbs are
// in internal/cli). The project and the server are resolved for the working directory only
// once the arguments pass the usage check, so `cairn task` with no verb prints the usage
// whether or not a project is set. No store is opened: the calls go through internal/client.
//
// Exit codes: 0 when the answer was printed, 1 when the project, the server or the call
// failed, 2 for a usage error.
func runTask(args []string, stdout, stderr io.Writer) int {
	return cli.Task(func() (cli.Session, error) {
		wd, err := workDir()
		if err != nil {
			return cli.Session{}, fmt.Errorf("the working directory: %w", err)
		}
		t, err := dial(wd, 0)
		if err != nil {
			return cli.Session{}, err
		}
		return cli.Session{Svc: t.svc, Ctx: t.ctx}, nil
	}, args, os.Stdin, stdout, stderr)
}
