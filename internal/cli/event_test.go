package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// TestEventLogHuman checks the human output of event log: the entry's time, kind and text, and
// that the request carries the step ref, the kind, the text and the caller.
func TestEventLogHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	at := time.Date(2026, 10, 30, 9, 2, 0, 0, time.UTC)
	svc := &fakeWrites{event: &core.Event{ID: 9, TaskID: "task-20261030-01", Kind: core.EventNote, Text: "started the step", OccurredAt: at}}
	code, out, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "note", "--text", "started the step", "--step", "1", "--agent", "hicks")
	if code != 0 || errOut != "" || out != stamp(at)+"  note  started the step\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and the entry's line", code, out, errOut)
	}
	got := svc.gotEvent
	if got.TaskID != "task-20261030-01" || got.Kind != core.EventNote || got.Text != "started the step" || got.Step == nil || got.Step.Number == nil || *got.Step.Number != 1 {
		t.Errorf("the request was %+v, want task, kind note, text and step 1", got)
	}
	if agentOf(t, svc.ctx) != "hicks" {
		t.Errorf("the call ran as %q, want hicks", agentOf(t, svc.ctx))
	}
}

// TestEventLogJSON checks the --json output of event log: one line, decoding to the event.
func TestEventLogJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{event: &core.Event{ID: 9, Kind: core.EventNote, Text: "started"}}
	code, out, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "note", "--text", "started", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, output %q; want one line and exit 0", code, errOut, out)
	}
	var got core.Event
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != 9 {
		t.Errorf("--json decodes to %+v (%v), want event 9", got, err)
	}
}

// TestEventLogReviewData checks that a review's data reaches the request as the JSON object it
// was given, from the flag and from standard input.
func TestEventLogReviewData(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	const review = `{"verdict":"approved","critical":0,"warnings":2}`
	svc := &fakeWrites{event: &core.Event{ID: 9, Kind: core.EventReview, Text: "reviewed"}}
	if code, _, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "review", "--text", "reviewed", "--data", review, "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if string(svc.gotEvent.Data) != review {
		t.Errorf("data %s, want the review as given", svc.gotEvent.Data)
	}
	if code, _, errOut := runWrite(t, Event, svc, review+"\n", "log", "task-20261030-01", "--kind", "review", "--text", "reviewed", "--data", "-", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if string(svc.gotEvent.Data) != review {
		t.Errorf("data from standard input %s, want the review without its line break", svc.gotEvent.Data)
	}
}

// TestEventLogByStepID checks that event log names its step by --step-id: the request carries an id
// ref and no number, and the human output is the entry's line.
func TestEventLogByStepID(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	at := time.Date(2026, 11, 5, 9, 2, 0, 0, time.UTC)
	svc := &fakeWrites{event: &core.Event{ID: 9, Kind: core.EventNote, Text: "started", OccurredAt: at}}
	code, out, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "note", "--text", "started", "--step-id", "412", "--agent", "hicks")
	if code != 0 || errOut != "" || out != stamp(at)+"  note  started\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and the entry's line", code, out, errOut)
	}
	if ref := svc.gotEvent.Step; ref == nil || ref.ID == nil || *ref.ID != 412 || ref.Number != nil {
		t.Errorf("the step ref is %+v, want the id 412 and no number", ref)
	}
}

// TestEventLogByStepIDJSONAndRefusal checks the --json output of an event on a step named by its id,
// and that a refusal of the server is one line on stderr, with the exit code of its kind (4 here).
func TestEventLogByStepIDJSONAndRefusal(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{event: &core.Event{ID: 9, Kind: core.EventNote, Text: "started"}}
	code, out, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "note", "--text", "started", "--step-id", "412", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, output %q; want one line and exit 0", code, errOut, out)
	}
	var got core.Event
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != 9 {
		t.Errorf("--json decodes to %+v (%v), want event 9", got, err)
	}
	svc.err = core.NotFound("step 412 is not in task task-20261030-01")
	code, out, errOut = runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "note", "--text", "started", "--step-id", "412", "--agent", "hicks")
	if code != 4 || out != "" || errOut != "cairn event log: step 412 is not in task task-20261030-01\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 4 (not_found) and the one-line refusal", code, out, errOut)
	}
}

// TestEventLogStepUsageErrors checks the usage errors of the step of event log: both --step and
// --step-id, an id below 1. Each exits 2 and makes no call.
func TestEventLogStepUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"number and id together", []string{"log", "task-20261030-01", "--kind", "note", "--text", "x", "--step", "1", "--step-id", "412", "--agent", "hicks"}, "not both"},
		{"id below one", []string{"log", "task-20261030-01", "--kind", "note", "--text", "x", "--step-id", "0", "--agent", "hicks"}, "--step-id must be a step id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{event: &core.Event{}}
			code, _, errOut := runWrite(t, Event, svc, "", c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestEventLogDataMustBeAJSONObject checks a --data that is not one JSON object, with no call. An empty or
// blank value is a usage error, exit 2, naming --data. Text that is not JSON, an array, a string and a JSON
// object followed by more text are malformed data, which core refuses as invalid, exit 3: the message names
// --data and gives core's fix. A JSON object with edge spaces is accepted.
func TestEventLogDataMustBeAJSONObject(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	usage := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"blank", "   "},
	}
	for _, c := range usage {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{event: &core.Event{}}
			code, _, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "review", "--text", "x", "--data", c.data, "--agent", "hicks")
			if code != 2 || !strings.Contains(errOut, "--data") || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming --data and no call", code, errOut, svc.calls)
			}
		})
	}
	invalid := []struct {
		name string
		data string
	}{
		{"not JSON", "verdict=approved"},
		{"an array", `[{"verdict":"approved"}]`},
		{"a string", `"approved"`},
		{"an object then more text", `{"verdict":"approved"} trailing`},
	}
	for _, c := range invalid {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{event: &core.Event{}}
			code, _, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "review", "--text", "x", "--data", c.data, "--agent", "hicks")
			if code != 3 || !strings.Contains(errOut, "--data: a review needs data") || !strings.Contains(errOut, "fix the data and log the review again") || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want exit 3 (invalid) naming --data, core's fix, and no call", code, errOut, svc.calls)
			}
		})
	}
	svc := &fakeWrites{event: &core.Event{ID: 9}}
	if code, _, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "review", "--text", "x", "--data", ` {"verdict":"approved","critical":0,"warnings":0} `, "--agent", "hicks"); code != 0 {
		t.Errorf("an object with edge spaces: exit %d, stderr %q; want it accepted", code, errOut)
	}
}

// TestEventLogDataFromStandardInputMustBeAJSONObject checks the same refusal for --data -: a
// standard input that is not one JSON object is refused as invalid, naming --data, and no call is made.
func TestEventLogDataFromStandardInputMustBeAJSONObject(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{event: &core.Event{}}
	code, _, errOut := runWrite(t, Event, svc, "not a review\n", "log", "task-20261030-01", "--kind", "review", "--text", "x", "--data", "-", "--agent", "hicks")
	if code != 3 || !strings.Contains(errOut, "--data") || svc.calls != 0 {
		t.Errorf("exit %d, stderr %q, calls %d; want exit 3 naming --data and no call", code, errOut, svc.calls)
	}
}

// TestEventLogRefusedByServer checks a refusal of the server: one line on stderr, exit 3 (invalid).
func TestEventLogRefusedByServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{err: core.Invalid("an agent may not log started: the server writes it")}
	code, out, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "started", "--text", "x", "--agent", "hicks")
	if code != 3 || out != "" || errOut != "cairn event log: an agent may not log started: the server writes it\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 3 (invalid) and the refusal", code, out, errOut)
	}
}

// TestEventLogUsageErrors checks the usage errors of event log: no task id, a step below 1, no
// agent, and two flags that read standard input. Each exits 2 and makes no call.
func TestEventLogUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no task id", "", []string{"log", "--kind", "note", "--text", "x", "--agent", "hicks"}, "name one task id"},
		{"step zero", "", []string{"log", "task-20261030-01", "--kind", "note", "--text", "x", "--step", "0", "--agent", "hicks"}, "--step must be a step number"},
		{"no agent", "", []string{"log", "task-20261030-01", "--kind", "note", "--text", "x"}, "no agent name"},
		{"two flags read standard input", "text", []string{"log", "task-20261030-01", "--kind", "review", "--text", "-", "--data", "-", "--agent", "hicks"}, "both read standard input"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{event: &core.Event{}}
			code, _, errOut := runWrite(t, Event, svc, c.stdin, c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestEventVerbs checks the usage errors of the command itself: no verb, and a verb it does not
// have.
func TestEventVerbs(t *testing.T) {
	svc := &fakeWrites{}
	if code, _, errOut := runWrite(t, Event, svc, ""); code != 2 || !strings.Contains(errOut, "cairn event log") {
		t.Errorf("no verb: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runWrite(t, Event, svc, "", "frobnicate"); code != 2 || !strings.Contains(errOut, `unknown verb "frobnicate"`) {
		t.Errorf("an unknown verb: exit %d, stderr %q", code, errOut)
	}
}
