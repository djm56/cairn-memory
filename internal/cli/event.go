package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
)

// Event is the sub-command `cairn event`: the journal of a task, for a person or an agent with
// only a shell (issue #87). Its verb is log, a write through event_log (CONTRACT §4). Each verb
// takes --json, which writes the result as one line of JSON, the object the MCP tool returns.
//
// Exit codes: 0 when the event was written, the code of the error kind when the open or the call failed (ExitCodeFor), 2 for a usage
// error.
func Event(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "cairn event: name what to do: cairn event log")
		return 2
	}
	verb, rest := args[0], args[1:]
	if verb == "log" {
		return eventLog(open, rest, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "cairn event: unknown verb %q: use cairn event log\n", verb)
	return 2
}

// eventLog is `cairn event log <task-id> --kind k --text t [--step n | --step-id n] [--data d]
// [--agent a] [--json]`: one journal entry on a task (CONTRACT §4, event_log). The step is named by
// its number, or by its id, and not both (CONTRACT §1.4). An agent may log note,
// blocked, unblocked, handoff, decision, review and custom. A review needs --data, a JSON object
// with verdict, critical and warnings. The server refuses the other kinds, which it writes itself.
func eventLog(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn event log"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	kind := fl.String("kind", "", "note, blocked, unblocked, handoff, decision, review or custom")
	text := fl.String("text", "", "what happened; - reads standard input")
	step := fl.Int("step", 0, "the step the event is about, by its number; left out, it is about the task")
	stepID := fl.Int64("step-id", 0, "the step the event is about, by its id, instead of its number")
	data := fl.String("data", "", "a JSON object of structured data, such as a review's verdict, critical and warnings; - reads standard input")
	pos, code, done := parse(fl, args, stdout, stderr)
	if done {
		return code
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, verb+": name one task id: cairn event log <task-id> --kind note --text t")
		return 2
	}
	if given(fl, "step") && *step < 1 {
		fmt.Fprintln(stderr, verb+": --step must be a step number, 1 or more; leave it out for the task")
		return 2
	}
	if given(fl, "step-id") && *stepID < 1 {
		fmt.Fprintln(stderr, verb+": --step-id must be a step id, 1 or more; leave it out for the task")
		return 2
	}
	if given(fl, "step") && given(fl, "step-id") {
		fmt.Fprintln(stderr, verb+": give the step by --step or by --step-id, not both")
		return 2
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	in := &inputs{stdin: stdin}
	req := core.EventLogRequest{TaskID: pos[0], Kind: core.EventKind(*kind)}
	var err error
	if req.Text, err = in.text("text", *text); err != nil {
		return usageErr(stderr, verb, err)
	}
	if given(fl, "step") {
		ref := core.NumberRef(*step)
		req.Step = &ref
	}
	if given(fl, "step-id") {
		ref := core.IDRef(*stepID)
		req.Step = &ref
	}
	if given(fl, "data") {
		raw, err := in.text("data", *data)
		if err != nil {
			return usageErr(stderr, verb, err)
		}
		if strings.TrimSpace(raw) == "" {
			return usageErr(stderr, verb, errors.New("--data is empty: give a JSON object, such as {\"verdict\": \"approved\", \"critical\": 0, \"warnings\": 0}, or leave the flag out"))
		}
		// core checks the data and gives the message for each fault, with the fix the contract names (CONTRACT
		// §4). The call is refused here, before any server is opened, as invalid. The flag is named in front.
		if err := core.CheckEventData(req.Kind, json.RawMessage(raw)); err != nil {
			return fail(stdout, stderr, verb, *asJSON, dataFlagError(err))
		}
		req.Data = json.RawMessage(raw)
	}
	s, err := open()
	if err != nil {
		return fail(stdout, stderr, verb, *asJSON, err)
	}
	ev, err := s.Svc.EventLog(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stdout, stderr, verb, *asJSON, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, ev)
	}
	fmt.Fprintln(stdout, eventLine(*ev))
	return 0
}

// dataFlagError returns the error of a --data that core refused, with --data named in front of core's
// message. The kind is core's, so the call exits with the code of invalid. An error that is not a core
// error is returned as it is.
func dataFlagError(err error) error {
	var ce *core.Error
	if !errors.As(err, &ce) {
		return err
	}
	return &core.Error{Kind: ce.Kind, Message: "--data: " + ce.Message}
}

// eventLine is one journal entry as a person reads it: the time to the minute, the kind and the
// text. The text is folded to one line.
func eventLine(e core.Event) string {
	return stamp(e.OccurredAt) + "  " + FoldLine(string(e.Kind)) + "  " + FoldLine(e.Text)
}
