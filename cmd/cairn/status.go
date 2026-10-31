package main

import (
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// runStatus is the sub-command `cairn status`: the project, the server in use and
// the active task, for a terminal (DESIGN.md §5.2). It reads only. The read of the
// active task creates the project's row on the server if it has none, as every
// first call does (CONTRACT §9 item 38); it writes no task, step or event.
//
// Exit codes: 0 when the server answered, 1 when the project, the server or the call
// failed, 2 for a usage error.
func runStatus(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "cairn status: takes no arguments, got %q\n", args)
		return 2
	}
	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn status: the working directory: %v\n", err)
		return 1
	}
	t, err := dial(wd, 0)
	if err != nil {
		fmt.Fprintf(stderr, "cairn status: %s\n", reason(err))
		return 1
	}
	cur, err := t.svc.TaskCurrent(t.ctx)
	if err != nil {
		fmt.Fprintf(stderr, "cairn status: %s\n", reason(err))
		return 1
	}
	writeStatus(stdout, t, cur)
	return 0
}

// writeStatus prints the status block: where the project and the server come from,
// then the active task, its next action and its current step.
func writeStatus(w io.Writer, t *target, cur *core.TaskCurrent) {
	fmt.Fprintf(w, "project     %s (from %s)\n", t.project.Name, projectSource(t.project))
	fmt.Fprintf(w, "server      %s (from %s)\n", t.url, serverSource())
	if cur.Task == nil {
		fmt.Fprintln(w, "task        none active")
		return
	}
	task := cur.Task
	fmt.Fprintf(w, "task        %s  %s  %s\n", task.ID, task.Status, task.Title)
	if task.NextAction != "" {
		fmt.Fprintf(w, "next        %s\n", task.NextAction)
	}
	if s := currentStep(cur.Steps); s != nil {
		fmt.Fprintf(w, "step        %d of %d: %s (%s)\n", s.Number, len(cur.Steps), s.Title, s.Status)
	}
	if cur.OtherActive > 0 {
		fmt.Fprintf(w, "other       %d other active task(s)\n", cur.OtherActive)
	}
	fmt.Fprintf(w, "directives  %d apply to this project\n", len(cur.Directives))
}

// currentStep returns the step to work on: the active one, else the first pending
// one, else nil when no step is open. Steps are in their order.
func currentStep(steps []core.Step) *core.Step {
	for _, want := range []core.StepStatus{core.StepActive, core.StepPending} {
		for i := range steps {
			if steps[i].Status == want {
				return &steps[i]
			}
		}
	}
	return nil
}

// projectSource says where the project name came from: the variable, or the file.
func projectSource(p identity.Project) string {
	if p.Source == identity.SourceEnv {
		return identity.EnvProject
	}
	return p.Path
}
