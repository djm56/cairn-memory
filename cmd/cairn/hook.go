package main

import (
	"bytes"
	"fmt"
	"io"
	"runtime/debug"
	"time"

	"github.com/djm56/cairn-memory/internal/cli"
	"github.com/djm56/cairn-memory/internal/core"
)

// sessionStartTimeout bounds the one call a session start makes. A hook that waits
// on a server that does not answer holds up the session, so it gives up well before
// the client's default of 30 seconds. It is a variable so a test can wait on a server
// that never answers, rather than the real five seconds.
var sessionStartTimeout = 5 * time.Second

// runHook is the sub-command `cairn hook <event>`: the output a client's hook reads
// (DESIGN.md §5.2). session-start is built. pre-compact and stop are M4 and are named
// as not built.
func runHook(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn hook: name the event, as cairn hook session-start")
		return 2
	}
	switch args[0] {
	case "session-start":
		return runSessionStart(args[1:], stdout, stderr)
	case "pre-compact", "stop":
		fmt.Fprintf(stderr, "cairn hook: %s is not built yet\n", args[0])
		return 2
	}
	fmt.Fprintf(stderr, "cairn hook: unknown event %q: use session-start\n", args[0])
	return 2
}

// runSessionStart prints the context a SessionStart hook adds to a session (DESIGN.md
// §4.1): the active task, its next action and current step, the directive count, and
// a line that tells the agent to call task_current. stdout carries only that text.
//
// Once its arguments are right, it exits 0 whatever happens after. A hook that fails
// the session start is worse than a missing context line. So when the project or the
// server is not there, or does not answer, nothing is printed on stdout and the reason
// goes to stderr. A panic is the same: it is reported on stderr, and the session starts.
// It writes nothing: its one call is a read.
func runSessionStart(args []string, stdout, stderr io.Writer) (code int) {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "cairn hook session-start: takes no arguments, got %q\n", args)
		return 2
	}
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(stderr, "cairn hook session-start: no context added: an internal fault: %v\n%s", p, debug.Stack())
			code = 0
		}
	}()
	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn hook session-start: no context added: the working directory: %v\n", err)
		return 0
	}
	t, err := dial(wd, sessionStartTimeout)
	if err != nil {
		fmt.Fprintf(stderr, "cairn hook session-start: no context added: %s\n", reason(err))
		return 0
	}
	cur, err := currentFor(t)
	if err != nil {
		fmt.Fprintf(stderr, "cairn hook session-start: no context added: %s\n", reason(err))
		return 0
	}
	// The block is built whole and written once, so a fault cannot leave part of it on
	// stdout.
	var block bytes.Buffer
	writeSessionStart(&block, t.project.Name, cur, t.role, t.roleRefused)
	_, _ = stdout.Write(block.Bytes())
	return 0
}

// writeSessionStart writes the session-start block for cur, with the hook's closing line. The block is
// the one cli.SessionStart writes, which prime writes too, so the two cannot drift.
func writeSessionStart(w io.Writer, project string, cur *core.TaskCurrent, role string, refused bool) {
	cli.SessionStart(w, project, cur, role, refused, cli.HookPointer)
}
