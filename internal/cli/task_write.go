package cli

import (
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/core"
)

// startInput is the JSON form of the structured fields of task_start (CONTRACT §2): the
// acceptance criteria, the key files the brief names, and the plan. --json-input reads it.
// A key it does not name is refused.
type startInput struct {
	Criteria []string         `json:"criteria"`
	KeyFiles []core.TaskFile  `json:"key_files"`
	Steps    []core.StepInput `json:"steps"`
}

// taskStart is `cairn task start --title t --goal g [--context c] [--constraints k] [--priority p]
// [--queue] [--json-input] [--agent a] [--json]`: a new task, with its brief and its plan (CONTRACT
// §2, task_start). The server refuses a blank title or goal, and a second active task unless the
// project allows parallel tasks. The structured fields come from standard input as one JSON object
// when --json-input is given.
func taskStart(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn task start"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	title := fl.String("title", "", "the task's short name; - reads standard input")
	goal := fl.String("goal", "", "what done looks like, in a sentence or two; - reads standard input")
	ctxFlag := fl.String("context", "", "why now, background and links; - reads standard input")
	constraints := fl.String("constraints", "", "what must not change; - reads standard input")
	priority := fl.String("priority", "", "low, normal, high or urgent; normal by default")
	queue := fl.Bool("queue", false, "create the task open, not active; task update starts it")
	jsonInput := fl.Bool("json-input", false, "read criteria, key_files and steps from standard input as one JSON object")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "%s: takes no task id, got %q\n", verb, pos)
		return 2
	}
	in := &inputs{stdin: stdin}
	req := core.TaskStartRequest{Priority: core.Priority(*priority), Queue: *queue}
	var err error
	if req.Title, err = in.text("title", *title); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Goal, err = in.text("goal", *goal); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Context, err = in.text("context", *ctxFlag); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.Constraints, err = in.text("constraints", *constraints); err != nil {
		return usageErr(stderr, verb, err)
	}
	if *jsonInput {
		var body startInput
		if err := in.decode("json-input", &body); err != nil {
			return usageErr(stderr, verb, err)
		}
		req.Criteria, req.KeyFiles, req.Steps = body.Criteria, body.KeyFiles, body.Steps
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	res, err := s.Svc.TaskStart(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, res)
	}
	writeStarted(stdout, res)
	return 0
}

// closeInput is the JSON form of the structured fields of task_close (CONTRACT §2): the criteria
// results, the lessons, the mistakes and the deliverables. A list that is left out is absent, which
// the close policy refuses for lessons and mistakes; [] says there are none. A key it does not name
// is refused.
type closeInput struct {
	Criteria     []core.CriterionOutcome `json:"criteria"`
	Lessons      []core.Lesson           `json:"lessons"`
	Mistakes     []core.Mistake          `json:"mistakes"`
	Deliverables []core.TaskFile         `json:"deliverables"`
}

// taskClose is `cairn task close <id> --outcome done|failed --summary s [--no-lessons-reason r]
// [--json-input] [--agent a] [--json]`: the task closed with its outcome (CONTRACT §2, task_close).
// A refused close changes nothing, and the refusal names what is missing. The structured fields
// come from standard input as one JSON object when --json-input is given.
func taskClose(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn task close"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	outcome := fl.String("outcome", "", "done or failed")
	summary := fl.String("summary", "", "what was done, in a few sentences; - reads standard input")
	noLessons := fl.String("no-lessons-reason", "", "why there are no lessons, when the lessons are []; - reads standard input")
	jsonInput := fl.Bool("json-input", false, "read criteria, lessons, mistakes and deliverables from standard input as one JSON object; a list left out is absent")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, verb+": name one task id: cairn task close <id> --outcome done --summary s")
		return 2
	}
	in := &inputs{stdin: stdin}
	req := core.TaskCloseRequest{ID: pos[0], Outcome: core.TaskStatus(*outcome)}
	var err error
	if req.Summary, err = in.text("summary", *summary); err != nil {
		return usageErr(stderr, verb, err)
	}
	if req.NoLessonsReason, err = in.text("no-lessons-reason", *noLessons); err != nil {
		return usageErr(stderr, verb, err)
	}
	if *jsonInput {
		var body closeInput
		if err := in.decode("json-input", &body); err != nil {
			return usageErr(stderr, verb, err)
		}
		req.Criteria, req.Lessons, req.Mistakes, req.Deliverables = body.Criteria, body.Lessons, body.Mistakes, body.Deliverables
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	t, err := s.Svc.TaskClose(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, t)
	}
	writeTask(stdout, t)
	return 0
}

// writeStarted prints a started task for a person: its id and status, then its plan.
func writeStarted(w io.Writer, res *core.TaskStartResult) {
	fmt.Fprintf(w, "%s  %s\n", FoldLine(res.ID), FoldLine(string(res.Status)))
	writeSteps(w, res.Steps)
}
