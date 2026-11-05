package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/djm56/cairn-memory/internal/core"
)

// Step is the sub-command `cairn step`: the steps of a task, for a person or an agent with only a
// shell (issue #87). Its verbs are add, update and plan. A step is named by its number,
// as the steps list shows it (CONTRACT §1.4). Each verb takes --json, which writes the result as one
// line of JSON, the object the MCP tool of the same name returns (CONTRACT §3).
//
// Exit codes: 0 when the step was written, 1 when the open or the call failed, 2 for a usage error.
func Step(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn step: name what to do: cairn step add|update|plan")
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "add":
		return stepAdd(open, rest, stdin, stdout, stderr)
	case "update":
		return stepUpdate(open, rest, stdin, stdout, stderr)
	case "plan":
		return stepPlan(open, rest, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn step: unknown verb %q: use cairn step add|update|plan\n", verb)
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
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
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
// step_update). finish, fail and skip need a summary. --assignee reassigns the step. The step is
// named by its number, or by its id with --step-id in place of the number (CONTRACT §1.4).
func stepUpdate(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn step update"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	action := fl.String("action", "", "start, finish, fail or skip")
	summary := fl.String("summary", "", "the result, needed for finish, fail and skip; - reads standard input")
	assignee := fl.String("assignee", "", "reassign the step to this agent")
	stepID := fl.Int64("step-id", 0, "the step by its id, instead of its number; give one or the other")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	ref, ok := stepUpdateRef(fl, pos, verb, *stepID, stderr)
	if !ok {
		return 2
	}
	req := core.StepUpdateRequest{TaskID: pos[0], Step: ref, Action: core.StepAction(*action)}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	in := &inputs{stdin: stdin}
	var err error
	if req.Summary, err = in.text("summary", *summary); err != nil {
		return usageErr(stderr, verb, err)
	}
	if given(fl, "assignee") {
		a := *assignee
		req.Agent = &a
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

// stepUpdateRef reads the step of a step update from its arguments. The task id is the first
// positional. The step is either the number in the second positional, or the id given with
// --step-id, and never both. A step id is 1 or more. It writes the usage error and returns ok
// false when the arguments do not give exactly one of the two.
func stepUpdateRef(fl *flag.FlagSet, pos []string, verb string, id int64, stderr io.Writer) (ref core.StepRef, ok bool) {
	if given(fl, "step-id") {
		// No task id is one fault and a number beside the id is another. Each gets its own message,
		// so the reader is not told to drop a form they did not give.
		if len(pos) == 0 {
			fmt.Fprintln(stderr, verb+": name the task id first: cairn step update <task-id> --step-id 412 --action start")
			return core.StepRef{}, false
		}
		if len(pos) > 1 {
			fmt.Fprintln(stderr, verb+": give the step by --step-id or by its number, not both: cairn step update <task-id> --step-id 412 --action start")
			return core.StepRef{}, false
		}
		if id < 1 {
			fmt.Fprintf(stderr, "%s: --step-id must be a step id, 1 or more: got %d\n", verb, id)
			return core.StepRef{}, false
		}
		return core.IDRef(id), true
	}
	if len(pos) != 2 {
		fmt.Fprintln(stderr, verb+": name a task id and a step number: cairn step update <task-id> <number> --action start")
		return core.StepRef{}, false
	}
	number, err := strconv.Atoi(pos[1])
	if err != nil || number < 1 {
		fmt.Fprintf(stderr, "%s: the step is a number, 1 or more, as the steps list shows it: got %q\n", verb, pos[1])
		return core.StepRef{}, false
	}
	return core.NumberRef(number), true
}

// planInput is the JSON form of step_plan (CONTRACT §3): the new pending steps, each with a title
// and an optional agent. "steps" is required: a plan with no steps list is refused, because it would
// otherwise remove every pending step by accident. [] removes them on purpose.
type planInput struct {
	Steps []core.StepInput `json:"steps"`
}

// stepPlan is `cairn step plan <task-id> --json-input [--agent a] [--json]`: the task's pending steps
// replaced with the list on standard input (CONTRACT §3, step_plan). A step that an event refers to
// cannot be removed, and the refusal names it. The list is only ever read from standard input, so
// --json-input is required.
func stepPlan(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn step plan"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	jsonInput := fl.Bool("json-input", false, "required: read the new steps from standard input as one JSON object, {\"steps\": [{\"title\": ..., \"agent\": ...}]}")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, verb+": name one task id: cairn step plan <task-id> --json-input")
		return 2
	}
	if !*jsonInput {
		fmt.Fprintln(stderr, verb+": the new steps come from standard input: give --json-input, with {\"steps\": [...]}")
		return 2
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	in := &inputs{stdin: stdin}
	var body planInput
	if err := in.decode("json-input", &body); err != nil {
		return usageErr(stderr, verb, err)
	}
	if body.Steps == nil {
		fmt.Fprintln(stderr, verb+": standard input has no steps list: give {\"steps\": [...]}, or [] to remove every pending step")
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	list, err := s.Svc.StepPlan(withAgent(s.Ctx, name), core.StepPlanRequest{TaskID: pos[0], Steps: body.Steps})
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, list)
	}
	if len(list.Steps) == 0 {
		fmt.Fprintln(stdout, "no steps")
		return 0
	}
	writeSteps(stdout, list.Steps)
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
