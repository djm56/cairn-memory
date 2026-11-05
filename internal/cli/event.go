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
// Exit codes: 0 when the event was written, 1 when the open or the call failed, 2 for a usage
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

// eventLog is `cairn event log <task-id> --kind k --text t [--step n] [--data d] [--agent a]
// [--json]`: one journal entry on a task (CONTRACT §4, event_log). An agent may log note,
// blocked, unblocked, handoff, decision, review and custom. A review needs --data, a JSON object
// with verdict, critical and warnings. The server refuses the other kinds, which it writes itself.
func eventLog(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const verb = "cairn event log"
	fl, asJSON := newFlags(verb)
	agent := fl.String("agent", "", agentHelp)
	kind := fl.String("kind", "", "note, blocked, unblocked, handoff, decision, review or custom")
	text := fl.String("text", "", "what happened; - reads standard input")
	step := fl.Int("step", 0, "the step the event is about, by its number; left out, it is about the task")
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
	if given(fl, "data") {
		raw, err := in.text("data", *data)
		if err != nil {
			return usageErr(stderr, verb, err)
		}
		if err := checkEventData(raw); err != nil {
			return usageErr(stderr, verb, err)
		}
		req.Data = json.RawMessage(raw)
	}
	name, ok := agentName(fl, *agent, true, verb, stderr)
	if !ok {
		return 2
	}
	s, err := open()
	if err != nil {
		return fail(stderr, verb, err)
	}
	ev, err := s.Svc.EventLog(withAgent(s.Ctx, name), req)
	if err != nil {
		return fail(stderr, verb, err)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, verb, ev)
	}
	fmt.Fprintln(stdout, eventLine(*ev))
	return 0
}

// checkEventData refuses a --data that is not one JSON object, before the project is resolved or
// the server is called (CONTRACT §4 takes a JSON object). Without this check a bad value reaches
// the encoder, which fails with a message that names no flag, and an empty value is dropped
// without a word. The message names --data.
func checkEventData(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("--data is empty: give a JSON object, such as {\"verdict\": \"approved\", \"critical\": 0, \"warnings\": 0}, or leave the flag out")
	}
	if trimmed[0] != '{' {
		return errors.New("--data must be a JSON object, which starts with {, such as {\"verdict\": \"approved\", \"critical\": 0, \"warnings\": 0}")
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return fmt.Errorf("--data is not valid JSON: %v", err)
	}
	return nil
}

// eventLine is one journal entry as a person reads it: the time to the minute, the kind and the
// text. The text is folded to one line.
func eventLine(e core.Event) string {
	return stamp(e.OccurredAt) + "  " + FoldLine(string(e.Kind)) + "  " + FoldLine(e.Text)
}
