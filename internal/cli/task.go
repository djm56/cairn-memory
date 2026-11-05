package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

// Session is what a command runs with: the service it calls and the caller's context,
// which carries the project and the client name. cmd/cairn builds it from the project and
// the server it resolves for the working directory.
type Session struct {
	Svc core.Service
	Ctx context.Context
}

// Opener returns the Session of a command. The command calls it only after its arguments
// pass the usage check, so a usage error needs no project and no server.
type Opener func() (Session, error)

// Task is the sub-command `cairn task`: the reads of a task, for a person or an agent with
// only a shell (issue #87). The verbs built so far are current, get and list. Each takes
// --json, which writes the result as one line of JSON: the object the MCP tool of the same
// name returns, with the same field names (CONTRACT §2).
//
// Exit codes: 0 when the answer was printed, 1 when the open or the call failed, 2 for a
// usage error. A failure is printed to stderr, as one line, in the words reason gives it.
func Task(open Opener, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn task: name what to do: cairn task current|get|list")
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "current":
		return taskCurrent(open, rest, stdout, stderr)
	case "get":
		return taskGet(open, rest, stdout, stderr)
	case "list":
		return taskList(open, rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn task: unknown verb %q: use cairn task current|get|list\n", verb)
	return 2
}

// taskCurrent is `cairn task current [--agent name] [--json]`: the active task, its
// current step and the directives that apply. With no active task it says so and exits 0,
// since that is an answer, not a failure.
func taskCurrent(open Opener, args []string, stdout, stderr io.Writer) int {
	fl, asJSON := newFlags("cairn task current")
	agent := fl.String("agent", "", "the calling agent's role, such as main or reviewer; names its agent-scoped directives")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "cairn task current: takes no task id, got %q\n", pos)
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, "cairn task current", err)
	}
	ctx := s.Ctx
	if *agent != "" {
		c, _ := core.CallerFrom(ctx)
		c.Agent = *agent
		ctx = core.WithCaller(ctx, c)
	}
	cur, err := s.Svc.TaskCurrent(ctx)
	if err != nil {
		return fail(stderr, "cairn task current", err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, "cairn task current", cur)
	}
	writeCurrent(stdout, cur)
	return 0
}

// taskGet is `cairn task get <id> [--section s] [--limit n] [--cursor c] [--json]`: one task.
// Without --section every part is read; --limit and --cursor page the events, as the
// server counts them (CONTRACT §2).
func taskGet(open Opener, args []string, stdout, stderr io.Writer) int {
	fl, asJSON := newFlags("cairn task get")
	section := fl.String("section", "", "one part of the task: brief, steps, events or close; empty for all")
	limit := fl.Int("limit", 0, "events only: the page size, 20 by default, 100 at most")
	cursor := fl.String("cursor", "", "events only: the next value from the last page")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "cairn task get: name one task id: cairn task get <id>")
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, "cairn task get", err)
	}
	d, err := s.Svc.TaskGet(s.Ctx, core.TaskGetRequest{
		ID: pos[0], Section: core.TaskSection(*section), Limit: *limit, Cursor: *cursor,
	})
	if err != nil {
		return fail(stderr, "cairn task get", err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, "cairn task get", d)
	}
	writeDetail(stdout, d)
	return 0
}

// taskList is `cairn task list [--status s]... [--limit n] [--cursor c] [--json]`: the
// project's tasks, most recently updated first. --status may be given more than once.
func taskList(open Opener, args []string, stdout, stderr io.Writer) int {
	fl, asJSON := newFlags("cairn task list")
	var statuses statusFlag
	fl.Var(&statuses, "status", "a status to list: open, active, blocked, done, failed or abandoned; repeat for more; empty lists all")
	limit := fl.Int("limit", 0, "the page size, 20 by default, 100 at most")
	cursor := fl.String("cursor", "", "the next value from the last page")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "cairn task list: takes no task id, got %q\n", pos)
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, "cairn task list", err)
	}
	list, err := s.Svc.TaskList(s.Ctx, core.TaskListRequest{Status: statuses, Limit: *limit, Cursor: *cursor})
	if err != nil {
		return fail(stderr, "cairn task list", err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, "cairn task list", list)
	}
	writeList(stdout, list)
	return 0
}

// newFlags returns the flag set of one verb and the --json flag every verb has. The flag
// package's own messages are discarded: parse prints the command's, as the other commands do.
func newFlags(name string) (*flag.FlagSet, *bool) {
	fl := flag.NewFlagSet(name, flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	asJSON := fl.Bool("json", false, "write the result as one line of JSON, the object the MCP tool returns")
	return fl, asJSON
}

// parse reads the flags of fl from args, in any order with the positional arguments, and
// returns the positional arguments. done is true when the command should stop now: the
// flags were asked for with -h (code 0, their list on stdout) or one was wrong (code 2,
// with the cause on stderr).
func parse(fl *flag.FlagSet, args []string, stdout, stderr io.Writer) (pos []string, code int, done bool) {
	for {
		if err := fl.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fl.SetOutput(stdout)
				fl.PrintDefaults()
				return nil, 0, true
			}
			fmt.Fprintf(stderr, "%s: %v; run %s -h for the flags\n", fl.Name(), err, fl.Name())
			return nil, 2, true
		}
		args = fl.Args()
		if len(args) == 0 {
			return pos, 0, false
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// fail prints the reason of err on one line to stderr, prefixed with the command, and
// returns the exit code of a failed call.
func fail(stderr io.Writer, command string, err error) int {
	fmt.Fprintf(stderr, "%s: %s\n", command, Reason(err))
	return 1
}

// writeJSON writes v as one line of JSON, with no HTML escapes, and returns the exit code.
// The field names and order are those of the Go type, so the output is stable across calls.
func writeJSON(stdout, stderr io.Writer, command string, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fail(stderr, command, err)
	}
	return 0
}

// statusFlag collects a repeatable --status flag into a list of task statuses.
type statusFlag []core.TaskStatus

// String returns the statuses given so far, comma separated. The flag package calls it.
func (s *statusFlag) String() string {
	parts := make([]string, 0, len(*s))
	for _, st := range *s {
		parts = append(parts, string(st))
	}
	return strings.Join(parts, ",")
}

// Set adds one status. Whether it is a real status is the server's to say, so the value
// is passed on as it is written.
func (s *statusFlag) Set(v string) error {
	*s = append(*s, core.TaskStatus(v))
	return nil
}

// writeCurrent prints the active task for a person: its line, next action, current step
// and directive counts. Each server text is folded to one line, so a title cannot start a
// line of its own.
func writeCurrent(w io.Writer, cur *core.TaskCurrent) {
	if cur.Task == nil {
		fmt.Fprintln(w, "no task is active")
		return
	}
	t := cur.Task
	fmt.Fprintf(w, "task        %s  %s  %s\n", Plain(t.ID), Plain(string(t.Status)), Plain(t.Title))
	if next := Plain(t.NextAction); next != "" {
		fmt.Fprintf(w, "next        %s\n", next)
	}
	if s := currentStep(cur.Steps); s != nil {
		fmt.Fprintf(w, "step        %d of %d: %s (%s)\n", s.Number, len(cur.Steps), Plain(s.Title), Plain(string(s.Status)))
	}
	if cur.OtherActive > 0 {
		fmt.Fprintf(w, "other       %d other active task(s)\n", cur.OtherActive)
	}
	fmt.Fprintf(w, "directives  %d apply to this project\n", len(cur.Directives))
	if cur.Expired > 0 {
		fmt.Fprintf(w, "expired     %d past review_by, left out\n", cur.Expired)
	}
	if cur.Omitted > 0 {
		fmt.Fprintf(w, "omitted     %d beyond the cap of %d\n", cur.Omitted, core.DirectiveCap)
	}
}

// currentStep returns the step to work on: the active one, else the first pending one,
// else nil. Steps are in their order.
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

// writeDetail prints one task in full for a person: its header, then each part the call
// read. A label is one line, so a multi-line goal is folded; the JSON keeps the text whole.
func writeDetail(w io.Writer, d *core.TaskDetail) {
	t := d.Task
	fmt.Fprintf(w, "%s  %s  %s  %s\n", FoldLine(t.ID), FoldLine(string(t.Status)), FoldLine(string(t.Priority)), FoldLine(t.Title))
	field(w, "project", t.Project)
	field(w, "goal", t.Goal)
	field(w, "context", t.Context)
	field(w, "constraints", t.Constraints)
	field(w, "next", t.NextAction)
	field(w, "blockers", t.Blockers)
	field(w, "outcome", t.OutcomeSummary)
	if len(d.Criteria) > 0 {
		fmt.Fprintln(w, "criteria")
		for _, c := range d.Criteria {
			line := fmt.Sprintf("  %d. %s", c.Ord, FoldLine(c.Criterion))
			if c.Result != "" {
				line += " — " + FoldLine(string(c.Result))
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(d.Steps) > 0 {
		fmt.Fprintln(w, "steps")
		for _, s := range d.Steps {
			line := fmt.Sprintf("  %d. [%s] %s", s.Number, FoldLine(string(s.Status)), FoldLine(s.Title))
			if s.Summary != "" {
				line += ": " + FoldLine(s.Summary)
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(d.Events) > 0 {
		fmt.Fprintln(w, "events, newest first")
		for _, e := range d.Events {
			fmt.Fprintf(w, "  %s  %s  %s\n", stamp(e.OccurredAt), FoldLine(string(e.Kind)), FoldLine(e.Text))
		}
	}
	if d.Next != "" {
		fmt.Fprintf(w, "more events: pass --cursor %s to read the next page\n", FoldLine(d.Next))
	}
	for _, l := range d.Lessons {
		fmt.Fprintf(w, "lesson      %s: %s\n", FoldLine(string(l.Kind)), FoldLine(l.Text))
	}
	if d.NoLessonsReason != "" {
		field(w, "no lessons", d.NoLessonsReason)
	}
	for _, m := range d.Mistakes {
		fmt.Fprintf(w, "mistake     %s (%s): %s\n", FoldLine(m.Agent), FoldLine(string(m.Impact)), FoldLine(m.Mistake))
	}
}

// field prints one labelled line, unless the value is empty.
func field(w io.Writer, label, value string) {
	if v := FoldLine(value); v != "" {
		fmt.Fprintf(w, "%-11s %s\n", label, v)
	}
}

// writeList prints one line per task, most recently updated first, and the cursor of the
// next page when there is one.
func writeList(w io.Writer, list *core.TaskList) {
	for _, t := range list.Tasks {
		fmt.Fprintf(w, "%s  %s  %s  %s\n", Plain(t.ID), Plain(string(t.Status)), Plain(string(t.Priority)), Plain(t.Title))
	}
	if list.Next != "" {
		fmt.Fprintf(w, "more tasks: pass --cursor %s to read the next page\n", FoldLine(list.Next))
	}
}

// stamp is a time as a person reads it: UTC, to the minute.
func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
