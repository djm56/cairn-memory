package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

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
// only a shell (issue #87). The reads are current, get and list; start is the first write,
// and update and close follow. Each takes --json, which writes the result as one line of
// JSON: the object the MCP tool of the same name returns, with the same field names
// (CONTRACT §2). A write takes its agent from --agent, or from CAIRN_AGENT.
//
// The text form is a summary for a person. It prints each section the call read, folds every
// text to one line, and leaves out what --json keeps: the timestamps, the event and step ids,
// and the fields of a lesson or a mistake beyond the ones shown. --json is the whole object.
//
// Exit codes: 0 when the answer was printed, 1 when the open or the call failed, 2 for a
// usage error. A failure is printed to stderr, as one line, in the words reason gives it.
func Task(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn task: name what to do: cairn task current|get|list|start|close")
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
	case "start":
		return taskStart(open, rest, stdin, stdout, stderr)
	case "close":
		return taskClose(open, rest, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn task: unknown verb %q: use cairn task current|get|list|start|close\n", verb)
	return 2
}

// taskCurrent is `cairn task current [--agent name] [--json]`: the active task, its
// current step and the directives that apply. With no active task it says so and exits 0,
// since that is an answer, not a failure.
func taskCurrent(open Opener, args []string, stdout, stderr io.Writer) int {
	fl, asJSON := newFlags("cairn task current")
	agent := fl.String("agent", "", "the calling agent's role, such as main or reviewer; names its agent-scoped directives; left out, "+EnvAgent+" is used")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "cairn task current: takes no task id, got %q\n", pos)
		return 2
	}
	name, _ := agentName(fl, *agent, false, "cairn task current", stderr)
	s, err := open()
	if err != nil {
		return fail(stderr, "cairn task current", err)
	}
	ctx := s.Ctx
	if name != "" {
		ctx = withAgent(ctx, name)
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
	limit := fl.Int("limit", 0, "events only: the page size: 20 by default, 100 at most; leave it out for the default")
	cursor := fl.String("cursor", "", "events only: the next value from the last page")
	step := fl.Int("step", 0, "events only: the events of one step, by its number as the steps list shows it")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if limitUsage(fl, "cairn task get", *limit, stderr) {
		return 2
	}
	if given(fl, "step") && *step < 1 {
		fmt.Fprintln(stderr, "cairn task get: --step must be a step number, 1 or more; leave it out for every step")
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "cairn task get: name one task id: cairn task get <id>")
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, "cairn task get", err)
	}
	req := core.TaskGetRequest{ID: pos[0], Section: core.TaskSection(*section), Limit: *limit, Cursor: *cursor}
	if given(fl, "step") {
		ref := core.NumberRef(*step)
		req.Step = &ref
	}
	d, err := s.Svc.TaskGet(s.Ctx, req)
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
	fl.Var(&statuses, "status", "a status to list: open, active, blocked, done, failed or abandoned; repeat for more; leave it out to list all")
	limit := fl.Int("limit", 0, "the page size: 20 by default, 100 at most; leave it out for the default")
	cursor := fl.String("cursor", "", "the next value from the last page")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if limitUsage(fl, "cairn task list", *limit, stderr) {
		return 2
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
	asJSON := fl.Bool("json", false, "write the whole result as one line of JSON, the object the MCP tool returns; the text form is a summary")
	return fl, asJSON
}

// parse reads the flags of fl from args, in any order with the positional arguments, and
// returns the positional arguments. Everything after a "--" is positional, as the flag package
// reads it, so a task id or a text that begins with a dash can be given after one. The tokens
// are sorted here, into flags with their values and positionals, and the flags are parsed once.
// done is true when the command should stop now: the flags were asked for with -h (code 0, their
// list on stdout) or one was wrong (code 2, with the cause on stderr).
func parse(fl *flag.FlagSet, args []string, stdout, stderr io.Writer) (pos []string, code int, done bool) {
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		if !strings.Contains(a, "=") && takesValue(fl, a) && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if err := fl.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fl.SetOutput(stdout)
			fl.PrintDefaults()
			return nil, 0, true
		}
		fmt.Fprintf(stderr, "%s: %v; run %s -h for the flags\n", fl.Name(), err, fl.Name())
		return nil, 2, true
	}
	return pos, 0, false
}

// takesValue reports whether the flag a, written without an "=", takes the next argument as its
// value. It mirrors the flag package, which reads that argument whatever it looks like, so a
// value may itself begin with a dash. A name the set does not have takes none; Parse refuses it.
func takesValue(fl *flag.FlagSet, a string) bool {
	f := fl.Lookup(strings.TrimLeft(a, "-"))
	if f == nil {
		return false
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !(ok && bf.IsBoolFlag())
}

// fail prints the reason of err on one line to stderr, prefixed with the command, and
// returns the exit code of a failed call.
func fail(stderr io.Writer, command string, err error) int {
	fmt.Fprintf(stderr, "%s: %s\n", command, Reason(err))
	return 1
}

// writeJSON writes v as one line of JSON, with no HTML escapes, and returns the exit code.
// The field names and order are those of the Go type, so the output is stable across calls.
// The text is server text, and a person may run --json on a terminal, so the characters a
// terminal can act on are written as escapes (escapeTerminal). The output still decodes to
// the same value.
func writeJSON(stdout, stderr io.Writer, command string, v any) int {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fail(stderr, command, err)
	}
	if _, err := stdout.Write(escapeTerminal(buf.Bytes())); err != nil {
		return fail(stderr, command, err)
	}
	return 0
}

// escapeTerminal returns the encoded JSON b with each character a terminal can act on written
// as a \uXXXX escape: DEL, the C1 controls U+0080 to U+009F (U+009B is the single-byte CSI), and
// the bidirectional embeddings and overrides U+202A to U+202E and isolates U+2066 to U+2069. The
// encoder writes these raw. Each can occur only inside a JSON string, since JSON's own syntax is
// ASCII, and an escape is valid there, so the result decodes to the same value.
func escapeTerminal(b []byte) []byte {
	var out bytes.Buffer
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if needsEscape(r) {
			fmt.Fprintf(&out, `\u%04x`, r)
		} else {
			out.Write(b[:size])
		}
		b = b[size:]
	}
	return out.Bytes()
}

// needsEscape reports whether r is one of the characters escapeTerminal writes as an escape.
func needsEscape(r rune) bool {
	switch {
	case r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// given reports whether the flag called name was set on the command line. A flag left out is
// not given, even when its value is the default.
func given(fl *flag.FlagSet, name string) bool {
	set := false
	fl.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// limitUsage prints the usage error for a --limit that was given as less than 1, and reports
// whether it did. An explicit 0 is refused rather than read as the default: the default is
// to leave the flag out.
func limitUsage(fl *flag.FlagSet, command string, limit int, stderr io.Writer) bool {
	if !given(fl, "limit") || limit >= 1 {
		return false
	}
	fmt.Fprintf(stderr, "%s: --limit must be 1 or more; leave it out for the default\n", command)
	return true
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
// is passed on as it is written. An empty value is refused here: leaving the flag out is how
// a person lists every status, and an empty status would only be refused by the server.
func (s *statusFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("a status must be named; leave --status out to list all")
	}
	*s = append(*s, core.TaskStatus(v))
	return nil
}

// writeCurrent prints the active task for a person: its header and body, the current step
// and the plan, the criteria, the recent events, and each directive that applies with its
// code, scope, title and rule. Every text is folded to one line, so a title cannot start a
// line of its own, and no text is cut. --json keeps the fields this summary leaves out.
func writeCurrent(w io.Writer, cur *core.TaskCurrent) {
	if cur.Task == nil {
		fmt.Fprintln(w, "no task is active")
		return
	}
	t := cur.Task
	fmt.Fprintf(w, "task        %s  %s  %s\n", FoldLine(t.ID), FoldLine(string(t.Status)), FoldLine(t.Title))
	writeBody(w, t)
	if s := currentStep(cur.Steps); s != nil {
		fmt.Fprintf(w, "step        %d of %d: %s (%s)\n", s.Number, len(cur.Steps), FoldLine(s.Title), FoldLine(string(s.Status)))
	}
	writeSteps(w, cur.Steps)
	writeCriteria(w, cur.Criteria)
	if cur.OtherActive > 0 {
		fmt.Fprintf(w, "other       %d other active task(s)\n", cur.OtherActive)
	}
	writeEvents(w, "recent events, newest first", cur.RecentEvents)
	writeDirectives(w, cur)
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

// writeDetail prints one task for a person: its header, then each section the call read.
// A label is one line, so a multi-line goal is folded; the JSON keeps the text whole.
func writeDetail(w io.Writer, d *core.TaskDetail) {
	t := d.Task
	writeTask(w, &t)
	writeCriteria(w, d.Criteria)
	writeFiles(w, "key files", d.KeyFiles)
	writeSteps(w, d.Steps)
	if len(d.Events) > 0 {
		writeEvents(w, "events, newest first", d.Events)
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
	writeFiles(w, "deliverables", d.Deliverables)
}

// writeTask prints one task for a person: its header, its id, status, priority and title, then the
// labelled fields of its body. A write prints the task this way, and task get does too.
func writeTask(w io.Writer, t *core.Task) {
	fmt.Fprintf(w, "%s  %s  %s  %s\n", FoldLine(t.ID), FoldLine(string(t.Status)), FoldLine(string(t.Priority)), FoldLine(t.Title))
	writeBody(w, t)
}

// writeBody prints the labelled fields of a task that both the summary and the detail share:
// the project, the goal, the context and constraints, the owner, the next action, the
// blockers and the outcome. A field that is empty is left out.
func writeBody(w io.Writer, t *core.Task) {
	field(w, "project", t.Project)
	field(w, "goal", t.Goal)
	field(w, "context", t.Context)
	field(w, "constraints", t.Constraints)
	field(w, "owner", t.Owner)
	field(w, "next", t.NextAction)
	field(w, "blockers", t.Blockers)
	field(w, "outcome", t.OutcomeSummary)
}

// field prints one labelled line, unless the value is empty.
func field(w io.Writer, label, value string) {
	if v := FoldLine(value); v != "" {
		fmt.Fprintf(w, "%-11s %s\n", label, v)
	}
}

// writeCriteria prints the acceptance criteria as a numbered block, each with its result and
// its evidence when they are set. It prints nothing when there are none.
func writeCriteria(w io.Writer, cs []core.Criterion) {
	if len(cs) == 0 {
		return
	}
	fmt.Fprintln(w, "criteria")
	for _, c := range cs {
		line := fmt.Sprintf("  %d. %s", c.Ord, FoldLine(c.Criterion))
		if c.Result != "" {
			line += " — " + FoldLine(string(c.Result))
		}
		if c.Evidence != "" {
			line += ": " + FoldLine(c.Evidence)
		}
		fmt.Fprintln(w, line)
	}
}

// writeSteps prints the steps of a plan, each with its number, status, title, assignee and
// summary when they are set. It prints nothing when there are none.
func writeSteps(w io.Writer, steps []core.Step) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(w, "steps")
	for _, s := range steps {
		fmt.Fprintln(w, "  "+stepLine(s))
	}
}

// writeFiles prints a block of files under its label: each path, then its note when it has
// one. It prints nothing when there are none. Key files and deliverables use it.
func writeFiles(w io.Writer, label string, files []core.TaskFile) {
	if len(files) == 0 {
		return
	}
	fmt.Fprintln(w, label)
	for _, f := range files {
		line := "  " + FoldLine(f.Path)
		if f.Note != "" {
			line += " — " + FoldLine(f.Note)
		}
		fmt.Fprintln(w, line)
	}
}

// writeEvents prints a block of journal events under its label, one line each: the time to the
// minute, the kind and the text. It prints nothing when there are none.
func writeEvents(w io.Writer, label string, events []core.Event) {
	if len(events) == 0 {
		return
	}
	fmt.Fprintln(w, label)
	for _, e := range events {
		fmt.Fprintln(w, "  "+eventLine(e))
	}
}

// writeDirectives prints the directive count of the active task, then each directive that
// applies: its code, its scope, its title, and its rule on the next line. The expired and
// omitted counts follow when they are not zero, as the MCP tool counts them.
func writeDirectives(w io.Writer, cur *core.TaskCurrent) {
	fmt.Fprintf(w, "directives  %d apply to this project\n", len(cur.Directives))
	for _, d := range cur.Directives {
		fmt.Fprintf(w, "  %s  %s  %s\n", FoldLine(d.Code), scopeLabel(d), FoldLine(d.Title))
		if rule := FoldLine(d.Rule); rule != "" {
			fmt.Fprintf(w, "          %s\n", rule)
		}
	}
	if cur.Expired > 0 {
		fmt.Fprintf(w, "expired     %d past review_by, left out\n", cur.Expired)
	}
	if cur.Omitted > 0 {
		fmt.Fprintf(w, "omitted     %d beyond the cap of %d\n", cur.Omitted, core.DirectiveCap)
	}
}

// scopeLabel names the scope of a directive for a person: global, project, or agent with the
// role it is scoped to.
func scopeLabel(d core.Directive) string {
	switch d.Scope {
	case core.ScopeAgent:
		return "agent " + FoldLine(d.Agent)
	case core.ScopeProject:
		return "project"
	}
	return FoldLine(string(d.Scope))
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
