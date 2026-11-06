package cli

import (
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// Recall is `cairn recall`: what a caller should know before a goal, for a person or an agent with
// only a shell (issue #87). It is the recall call of CONTRACT §6 and takes no verb. The answer has the
// directives that apply to the project and the role, ranked and capped as directive list gives them,
// and the lessons, mistakes and tasks that match the goal, from every project. It takes --json, which
// writes the result as one line of JSON: the object the MCP tool returns, with the same field names.
//
// --goal is required, and --goal - reads it from standard input. --agent names the caller's role, as
// the MCP tool's agent argument does: it names the role's agent-scoped directives, and it is not a
// filter on the rest. Left out, CAIRN_AGENT names the role, as task current reads it. With neither, no
// agent-scoped directive is listed. A name the wire form refuses, from either source, is a usage error
// here, exit 2, where status and the session-start hook only note a refused CAIRN_AGENT and go on.
//
// The text form is a summary. It prints the directives as directive list prints them, then one line for
// each lesson, mistake and task, and the empty-list line for each part with none. --json keeps every
// field of each part, including the task id and project of a lesson, which the summary leaves out.
//
// Exit codes: 0 when the answer was printed, the code of the error kind when the open or the call failed
// (ExitCodeFor), 2 for a usage error.
func Recall(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn recall"
	fl, asJSON := newFlags(verb)
	goal := fl.String("goal", "", "what the task is for, in a sentence; - reads standard input")
	agent := fl.String("agent", "", "the calling agent's role, such as hicks or reviewer; names its agent-scoped directives; left out, "+identity.EnvAgent+" is used; a name the wire form refuses is a usage error")
	limit := fl.Int("limit", 0, "per list: 5 by default, 100 at most; leave it out for the default")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if limitUsage(fl, verb, *limit, stderr) {
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no positional argument, got %q: give the goal with --goal\n", verb, pos)
		return 2
	}
	if !given(fl, "goal") {
		fmt.Fprintf(stderr, "%s: --goal is required: cairn recall --goal \"what the task is for\"\n", verb)
		return 2
	}
	name, ok := agentName(fl, *agent, false, verb, stderr)
	if !ok {
		return 2
	}
	in := &inputs{stdin: stdin}
	text, err := in.text("goal", *goal)
	if err != nil {
		return usageErr(stderr, verb, err)
	}
	s, err := open()
	if err != nil {
		return fail(stdout, stderr, verb, *asJSON, err)
	}
	res, err := s.Svc.Recall(s.Ctx, core.RecallRequest{Goal: text, Agent: name, Limit: *limit})
	if err != nil {
		return fail(stdout, stderr, verb, *asJSON, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, res)
	}
	writeRecall(stdout, res)
	return 0
}

// writeRecall prints the summary of a recall: the directives as directive list prints them, then one
// line for each lesson, mistake and task. A part with no item prints the empty-list line for it.
func writeRecall(w io.Writer, r *core.RecallResult) {
	writeDirectiveList(w, &core.DirectiveList{Directives: r.Directives, Expired: r.Expired, Omitted: r.Omitted})
	if len(r.Lessons) == 0 {
		fmt.Fprintln(w, emptyLine("lessons"))
	}
	for _, h := range r.Lessons {
		fmt.Fprintf(w, "lesson  %s  %s  %s: %s\n", Plain(h.TaskID), Plain(h.Project), Plain(string(h.Kind)), Plain(h.Text))
	}
	if len(r.Mistakes) == 0 {
		fmt.Fprintln(w, emptyLine("mistakes"))
	}
	for _, h := range r.Mistakes {
		fmt.Fprintf(w, "mistake  %s  %s  %s  %s: %s\n", Plain(h.TaskID), Plain(h.Project), Plain(h.Agent), Plain(string(h.Impact)), Plain(h.Mistake.Mistake))
	}
	if len(r.Tasks) == 0 {
		fmt.Fprintln(w, emptyLine("tasks"))
	}
	for _, t := range r.Tasks {
		fmt.Fprintf(w, "task  %s  %s  %s  %s\n", Plain(t.ID), Plain(t.Project), Plain(string(t.Status)), Plain(t.Title))
	}
}
