package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestEventLogHuman checks the human output of event log: the entry's time, kind and text, and
// that the request carries the step ref, the kind, the text and the caller.
func TestEventLogHuman(t *testing.T) {
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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

// TestEventLogRefusedByServer checks a refusal of the server: one line on stderr, exit 1.
func TestEventLogRefusedByServer(t *testing.T) {
	t.Setenv(EnvAgent, "")
	svc := &fakeWrites{err: core.Invalid("an agent may not log started: the server writes it")}
	code, out, errOut := runWrite(t, Event, svc, "", "log", "task-20261030-01", "--kind", "started", "--text", "x", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn event log: an agent may not log started: the server writes it\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1 and the refusal", code, out, errOut)
	}
}

// TestEventLogUsageErrors checks the usage errors of event log: no task id, a step below 1, no
// agent, and two flags that read standard input. Each exits 2 and makes no call.
func TestEventLogUsageErrors(t *testing.T) {
	t.Setenv(EnvAgent, "")
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
