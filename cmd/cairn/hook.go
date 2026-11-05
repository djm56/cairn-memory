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

// hookTimeout bounds the one call a hook makes. A hook that waits on a server that does not answer holds
// up the session or the turn, so it gives up well before the client's default of 30 seconds. It is a
// variable so a test can wait on a server that never answers, rather than the real five seconds.
var hookTimeout = 5 * time.Second

// hookWriter writes what one hook prints for the active task it has read. It writes nothing when it has
// nothing to say.
type hookWriter func(w io.Writer, t *target, cur *core.TaskCurrent)

// runHook is the sub-command `cairn hook <event>`: the output a client's hook reads (DESIGN.md §4.1 and
// §5.2). session-start and pre-compact are built and print the same block. stop is M4 and is named as not
// built.
func runHook(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn hook: name the event, as cairn hook session-start")
		return 2
	}
	switch args[0] {
	case "session-start", "pre-compact":
		return runContextHook(args[0], args[1:], stdout, stderr, writeContextBlock)
	case "stop":
		fmt.Fprintf(stderr, "cairn hook: %s is not built yet\n", args[0])
		return 2
	}
	fmt.Fprintf(stderr, "cairn hook: unknown event %q: use session-start or pre-compact\n", args[0])
	return 2
}

// runContextHook is the path the events share. It takes no arguments, makes one read of the active task,
// and hands the answer to write, which prints what the event prints.
//
// Once its arguments are right, it exits 0 whatever happens after. A hook that fails the session, or the
// turn, is worse than a missing line. So when the project or the server is not there, or does not answer,
// nothing is printed on stdout and the reason goes to stderr. A panic is the same: it is reported on
// stderr, and the client carries on. It writes nothing: its one call is a read.
func runContextHook(event string, args []string, stdout, stderr io.Writer, write hookWriter) (code int) {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "cairn hook %s: takes no arguments, got %q\n", event, args)
		return 2
	}
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(stderr, "cairn hook %s: no context added: an internal fault: %v\n%s", event, p, debug.Stack())
			code = 0
		}
	}()
	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn hook %s: no context added: the working directory: %v\n", event, err)
		return 0
	}
	t, err := dial(wd, hookTimeout)
	if err != nil {
		fmt.Fprintf(stderr, "cairn hook %s: no context added: %s\n", event, reason(err))
		return 0
	}
	cur, err := currentFor(t)
	if err != nil {
		fmt.Fprintf(stderr, "cairn hook %s: no context added: %s\n", event, reason(err))
		return 0
	}
	// The output is built whole and written once, so a fault cannot leave part of it on stdout.
	var out bytes.Buffer
	write(&out, t, cur)
	_, _ = stdout.Write(out.Bytes())
	return 0
}

// writeContextBlock is what session-start and pre-compact print: the session-start block for the active
// task, with the role the counts were read for.
func writeContextBlock(w io.Writer, t *target, cur *core.TaskCurrent) {
	writeSessionStart(w, t.project.Name, cur, t.role, t.roleRefused)
}

// writeSessionStart writes the session-start block for cur, with the hook's closing line. The block is
// the one cli.SessionStart writes, which prime writes too, so the two cannot drift.
func writeSessionStart(w io.Writer, project string, cur *core.TaskCurrent, role string, refused bool) {
	cli.SessionStart(w, project, cur, role, refused, cli.HookPointer, cli.HookOmittedCall)
}
