package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/djm56/cairn-memory/internal/core"
)

// Step is the sub-command `cairn step`: the steps of a task, for a person or an agent with only a
// shell (issue #87). Its verbs are add and update, and plan follows. A step is named by its number,
// as the steps list shows it (CONTRACT §1.4). Each verb takes --json, which writes the result as one
// line of JSON, the object the MCP tool of the same name returns (CONTRACT §3).
//
// Exit codes: 0 when the step was written, 1 when the open or the call failed, 2 for a usage error.
func Step(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn step: name what to do: cairn step add|update")
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "add":
		return stepAdd(open, rest, stdin, stdout, stderr)
	case "update":
		return stepUpdate(open, rest, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn step: unknown verb %q: use cairn step add|update\n", verb)
	return 2
}

// stepAdd is `cairn step add <task-id> --title t [--after n] [--assignee a] [--agent a] [--json]`:
// one step inserted into the plan (CONTRACT §3, step_add). Left out, --after puts the step last;
// 0 puts it first. --assignee is the agent the step is for, and --agent is the caller.
func stepAdd(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn step add"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	title := fl.String("title", "", "what the step does; - reads standard input")
	after := fl.Int("after", 0, "insert after the step with this number; 0 puts it first; left out, it goes last")
	assignee := fl.String("assignee", "", "the agent the step is for")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, verb+": name one task id: cairn step add <task-id> --title t")
		return 2
	}
	if given(fl, "after") && *after < 0 {
		fmt.Fprintln(stderr, verb+": --after must be a step number, 0 or more; leave it out to add the step last")
		return 2
	}
	in := &inputs{stdin: stdin}
	req := core.StepAddRequest{TaskID: pos[0], Agent: *assignee}
	var err error
	if req.Title, err = in.text("title", *title); err != nil {
		return usageErr(stderr, verb, err)
	}
	if given(fl, "after") {
		ref := core.NumberRef(*after)
		req.After = &ref
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	step, err := s.Svc.StepAdd(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, step)
	}
	fmt.Fprintln(stdout, stepLine(*step))
	return 0
}

// stepUpdate is `cairn step update <task-id> <number> --action a [--summary s] [--assignee a]
// [--agent a] [--json]`: one step started, finished, failed or skipped, or reassigned (CONTRACT §3,
// step_update). finish, fail and skip need a summary. --assignee reassigns the step.
func stepUpdate(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn step update"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	action := fl.String("action", "", "start, finish, fail or skip")
	summary := fl.String("summary", "", "the result, needed for finish, fail and skip; - reads standard input")
	assignee := fl.String("assignee", "", "reassign the step to this agent")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) != 2 {
		fmt.Fprintln(stderr, verb+": name a task id and a step number: cairn step update <task-id> <number> --action start")
		return 2
	}
	number, err := strconv.Atoi(pos[1])
	if err != nil || number < 1 {
		fmt.Fprintf(stderr, "%s: the step is a number, 1 or more, as the steps list shows it: got %q\n", verb, pos[1])
		return 2
	}
	in := &inputs{stdin: stdin}
	req := core.StepUpdateRequest{TaskID: pos[0], Step: core.NumberRef(number), Action: core.StepAction(*action)}
	if req.Summary, err = in.text("summary", *summary); err != nil {
		return usageErr(stderr, verb, err)
	}
	if given(fl, "assignee") {
		a := *assignee
		req.Agent = &a
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	step, err := s.Svc.StepUpdate(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, step)
	}
	fmt.Fprintln(stdout, stepLine(*step))
	return 0
}

// stepLine is one step as a person reads it: its number, its status, its title, its assignee when
// it has one, and its summary when it has one. Every text is folded to one line.
func stepLine(s core.Step) string {
	line := fmt.Sprintf("%d. [%s] %s", s.Number, FoldLine(string(s.Status)), FoldLine(s.Title))
	if s.Agent != "" {
		line += " (agent " + FoldLine(s.Agent) + ")"
	}
	if s.Summary != "" {
		line += ": " + FoldLine(s.Summary)
	}
	return line
}
