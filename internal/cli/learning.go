package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
)

// Learning is the sub-command `cairn learning`: the proposals a project keeps, for a person or an
// agent with only a shell (issue #87). Its verbs are add, which proposes one through learning_add,
// and list, which reads them through learning_list (CONTRACT §5). Each takes --json, which writes
// the result as one line of JSON: the object the MCP tool of the same name returns, with the same
// field names.
//
// The text form of add prints every field the learning holds, each folded to one line. The text
// form of list is a summary: one line per learning, with its id, status, kind, title and target.
// --json is the whole object.
//
// Exit codes: 0 when the answer was printed, 1 when the open or the call failed, 2 for a usage
// error.
func Learning(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn learning: name what to do: cairn learning add|list")
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "add":
		return learningAdd(open, rest, stdin, stdout, stderr)
	case "list":
		return learningList(open, rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn learning: unknown verb %q: use cairn learning add|list\n", verb)
	return 2
}

// learningKinds are the kinds a learning may have (CONTRACT §5). The check is made here, before
// the project is resolved, so a kind outside them is a usage error and not a refused call.
var learningKinds = []core.LearningKind{core.LearningFinding, core.LearningPattern, core.LearningAgentNote}

// learningAdd is `cairn learning add --kind k --title t --body b [--task-id id] [--target t]
// [--rationale r] [--example e] --agent a [--json]`: one proposal, stored as proposed (CONTRACT §5,
// learning_add). The kind, the title and the body are required; --title, --body, --target,
// --rationale and --example each take - to read standard input. The agent is --agent, or the
// CAIRN_AGENT variable.
func learningAdd(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn learning add"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	kind := fl.String("kind", "", "finding, pattern or agent-note")
	title := fl.String("title", "", "a short title; - reads standard input")
	body := fl.String("body", "", "the suggestion, the solution or the note; - reads standard input")
	taskID := fl.String("task-id", "", "the task the learning came from, by its id")
	target := fl.String("target", "", "what it would change: a file, a rule or an agent; - reads standard input")
	rationale := fl.String("rationale", "", "why it is proposed; - reads standard input")
	example := fl.String("example", "", "an example; - reads standard input")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no positional argument, got %q: give the task with --task-id\n", verb, pos)
		return 2
	}
	if !validLearningKind(*kind) {
		fmt.Fprintf(stderr, "%s: --kind must be finding, pattern or agent-note: cairn learning add --kind finding --title t --body b\n", verb)
		return 2
	}
	if !given(fl, "title") {
		fmt.Fprintf(stderr, "%s: --title is required: cairn learning add --kind finding --title t --body b\n", verb)
		return 2
	}
	if !given(fl, "body") {
		fmt.Fprintf(stderr, "%s: --body is required: cairn learning add --kind finding --title t --body b\n", verb)
		return 2
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	in := &inputs{stdin: stdin}
	req := core.LearningAddRequest{TaskID: *taskID, Kind: core.LearningKind(*kind)}
	var err error
	if req.Title, err = in.text("title", *title); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Body, err = in.text("body", *body); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Target, err = in.text("target", *target); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Rationale, err = in.text("rationale", *rationale); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Example, err = in.text("example", *example); err != nil {
		return usageErr(stderr, verb, err)
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	l, err := s.Svc.LearningAdd(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, l)
	}
	writeLearning(stdout, l)
	return 0
}

// learningList is `cairn learning list [--status s]... [--kind k]... [--target t] [--limit n]
// [--cursor c] [--json]`: the project's learnings, newest first (CONTRACT §5, learning_list). --status
// and --kind may each be given more than once; left out, every status or kind is listed. A --kind that
// is not one of the three learning kinds is a usage error, as it is for learning add, so it exits 2
// before the call. A --status is judged by the server. The list is the caller's project only: the
// all_projects argument is not offered here, as it needs an operator or viewer role that the CLI does
// not hold.
func learningList(open Opener, args []string, stdout, stderr io.Writer) int {
	const verb = "cairn learning list"
	fl, asJSON := newFlags(verb)
	statuses := listFlag[core.LearningStatus]{name: "status"}
	fl.Var(&statuses, "status", "a status to list: proposed, accepted, rejected, applied, retired or superseded; repeat for more; leave it out to list all")
	kinds := listFlag[core.LearningKind]{name: "kind"}
	fl.Var(&kinds, "kind", "a kind to list: finding, pattern or agent-note; repeat for more; leave it out to list all")
	target := fl.String("target", "", "only the learnings with this target")
	limit := fl.Int("limit", 0, "the page size: 20 by default, 100 at most; leave it out for the default")
	cursor := fl.String("cursor", "", "the next value from the last page")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if limitUsage(fl, verb, *limit, stderr) {
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no positional argument, got %q\n", verb, pos)
		return 2
	}
	for _, k := range kinds.vals {
		if !validLearningKind(string(k)) {
			fmt.Fprintf(stderr, "%s: --kind must be finding, pattern or agent-note, not %q: cairn learning list --kind finding\n", verb, k)
			return 2
		}
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	list, err := s.Svc.LearningList(s.Ctx, core.LearningListRequest{
		Status: statuses.vals, Kind: kinds.vals, Target: *target, Limit: *limit, Cursor: *cursor,
	})
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, list)
	}
	writeLearningList(stdout, list)
	return 0
}

// validLearningKind reports whether kind is one a learning may have.
func validLearningKind(kind string) bool {
	for _, k := range learningKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// listFlag collects a repeatable flag into a list of named values, such as learning statuses. Its
// empty value is refused, since leaving the flag out is how a person lists every value. Whether a
// value is a real one is the server's to say, so each is passed on as it is written, except where the
// verb checks its own kinds before the call. name is the flag's name, for that message.
type listFlag[T ~string] struct {
	name string
	vals []T
}

// String returns the values given so far, comma separated. The flag package calls it.
func (f *listFlag[T]) String() string {
	parts := make([]string, 0, len(f.vals))
	for _, v := range f.vals {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ",")
}

// Set adds one value. An empty value is refused: leave the flag out to list all.
func (f *listFlag[T]) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("a %s must be named; leave --%s out to list all", f.name, f.name)
	}
	f.vals = append(f.vals, T(v))
	return nil
}

// writeLearning prints one learning with every field it holds, one labelled line each, as a person
// reads it. The fields left empty are not printed. Every text is folded to one line.
func writeLearning(w io.Writer, l *core.Learning) {
	fmt.Fprintf(w, "learning    %d  %s  %s  %s\n", l.ID, FoldLine(string(l.Status)), FoldLine(string(l.Kind)), FoldLine(l.Title))
	field(w, "project", l.Project)
	field(w, "task", l.TaskID)
	field(w, "agent", l.Agent)
	field(w, "target", l.Target)
	field(w, "body", l.Body)
	field(w, "rationale", l.Rationale)
	field(w, "example", l.Example)
	fmt.Fprintf(w, "%-11s %s\n", "created", stamp(l.CreatedAt))
}

// writeLearningList prints one line per learning, newest first: its id, status, kind, title, and
// the target when it has one. It is a summary; the fields it leaves out are in --json. The cursor of
// the next page follows when there is one.
func writeLearningList(w io.Writer, list *core.LearningList) {
	for _, l := range list.Learnings {
		line := fmt.Sprintf("%d  %s  %s  %s", l.ID, Plain(string(l.Status)), Plain(string(l.Kind)), Plain(l.Title))
		if t := Plain(l.Target); t != "" {
			line += "  target " + t
		}
		fmt.Fprintln(w, line)
	}
	if list.Next != "" {
		fmt.Fprintf(w, "more learnings: pass --cursor %s to read the next page\n", FoldLine(list.Next))
	}
}
