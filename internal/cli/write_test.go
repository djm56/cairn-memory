package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// TestAgentFlagBeatsTheVariable checks that --agent names the caller of a read when the
// variable is set too: the flag is what the person typed for this call.
func TestAgentFlagBeatsTheVariable(t *testing.T) {
	t.Setenv(identity.EnvAgent, "hicks")
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	if code, _, errOut := runTask(t, svc, "current", "--agent", "reviewer"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if c, _ := core.CallerFrom(svc.gotCtx); c.Agent != "reviewer" {
		t.Errorf("the caller agent is %q, want reviewer from the flag", c.Agent)
	}
}

// TestAgentVariableIsTheReadFallback checks that a read with no --agent takes the agent from
// CAIRN_AGENT, so the agent-scoped directives of that role are named.
func TestAgentVariableIsTheReadFallback(t *testing.T) {
	t.Setenv(identity.EnvAgent, "hicks")
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	if code, _, errOut := runTask(t, svc, "current"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if c, _ := core.CallerFrom(svc.gotCtx); c.Agent != "hicks" {
		t.Errorf("the caller agent is %q, want hicks from %s", c.Agent, identity.EnvAgent)
	}
}

// TestBlankAgentIsNoAgentForARead checks that a name of only spaces is no name, and that a read
// still runs, with no agent, as CONTRACT §1.1 says.
func TestBlankAgentIsNoAgentForARead(t *testing.T) {
	t.Setenv(identity.EnvAgent, "   ")
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	code, _, errOut := runTask(t, svc, "current")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q; a read with a blank name should run", code, errOut)
	}
	if c, _ := core.CallerFrom(svc.gotCtx); c.Agent != "" {
		t.Errorf("the caller agent is %q, want none", c.Agent)
	}
}

// TestAgentNameForAWriteNamesBothSources checks the usage error of a write with no agent: it
// names the flag and the variable, and a blank flag is refused rather than replaced by the
// variable.
func TestAgentNameForAWriteNamesBothSources(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	fl := flag.NewFlagSet("cairn task start", flag.ContinueOnError)
	agent := fl.String("agent", "", "")
	if err := fl.Parse(nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if _, ok := agentName(fl, *agent, true, "cairn task start", &stderr); ok {
		t.Fatal("a write with no agent was accepted")
	}
	for _, want := range []string{"cairn task start: no agent name", "--agent", identity.EnvAgent} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q lacks %q", stderr.String(), want)
		}
	}

	t.Setenv(identity.EnvAgent, "reviewer")
	fl2 := flag.NewFlagSet("cairn task start", flag.ContinueOnError)
	blank := fl2.String("agent", "", "")
	if err := fl2.Parse([]string{"--agent", "  "}); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if _, ok := agentName(fl2, *blank, true, "cairn task start", &stderr); ok {
		t.Error("a blank --agent was replaced by the variable instead of refused")
	}
}

// TestAgentNameIsCheckedBeforeTheCall checks that an agent name the wire form cannot carry is a
// usage error, exit 2, before the project is opened and before any call. The name is refused from
// the flag and from the variable, for a write and for a read, with a trailing space and with a
// control character. The message names where the name came from, and does not print the control
// character raw.
func TestAgentNameIsCheckedBeforeTheCall(t *testing.T) {
	bad := []struct{ label, name string }{
		{"a trailing space", "hicks "},
		{"a control character", "hicks" + string(rune(1))},
	}
	for _, b := range bad {
		for _, source := range []string{"flag", "variable"} {
			for _, verb := range []string{"write", "read"} {
				t.Run(b.label+" from the "+source+" on a "+verb, func(t *testing.T) {
					t.Setenv(identity.EnvAgent, "")
					if source == "variable" {
						t.Setenv(identity.EnvAgent, b.name)
					}
					var cmd command
					var calls func() int
					var svc core.Service
					var args []string
					if verb == "write" {
						cmd = Event
						fw := &fakeWrites{event: &core.Event{}}
						svc, calls = fw, func() int { return fw.calls }
						args = []string{"log", "task-20261030-01", "--kind", "note", "--text", "x"}
					} else {
						cmd = Task
						ft := &fakeTasks{cur: &core.TaskCurrent{}}
						svc, calls = ft, func() int { return ft.calls }
						args = []string{"current"}
					}
					if source == "flag" {
						args = append(args, "--agent", b.name)
					}
					opened := false
					open := func() (Session, error) {
						opened = true
						return Session{Svc: svc, Ctx: core.WithCaller(context.Background(), testCaller)}, nil
					}
					var out, errOut bytes.Buffer
					code := cmd(open, args, strings.NewReader(""), &out, &errOut)
					if code != 2 || opened || calls() != 0 {
						t.Fatalf("exit %d, opened %v, calls %d; want a usage error with no open and no call", code, opened, calls())
					}
					wantSource := "--agent"
					if source == "variable" {
						wantSource = identity.EnvAgent
					}
					if !strings.Contains(errOut.String(), wantSource) || !strings.Contains(errOut.String(), "is refused") {
						t.Errorf("stderr %q does not name the source %s", errOut.String(), wantSource)
					}
					if strings.ContainsRune(errOut.String(), 1) {
						t.Errorf("stderr %q holds the control character raw", errOut.String())
					}
				})
			}
		}
	}
}

// countingReader is a standard input that records how many bytes a command read from it.
type countingReader struct {
	r io.Reader
	n int
}

// Read reads from the underlying reader and counts the bytes it returns.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// TestUsageErrorsBeforeStandardInput checks that a write verb refuses a missing agent, or a
// cheap range fault, before it reads standard input: a usage error that consumed the input would
// leave the person's piped text unsent, with no word about why. Each case gives a flag that reads
// standard input, exits 2, reads no byte of it, and opens no project.
func TestUsageErrorsBeforeStandardInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	const id = "task-20261030-01"
	cases := []struct {
		name string
		cmd  command
		args []string
	}{
		{"event log text from standard input, no agent", Event, []string{"log", id, "--kind", "note", "--text", "-"}},
		{"event log data from standard input, no agent", Event, []string{"log", id, "--kind", "review", "--text", "x", "--data", "-"}},
		{"event log with step zero", Event, []string{"log", id, "--kind", "note", "--text", "-", "--step", "0", "--agent", "hicks"}},
		{"step add title from standard input, no agent", Step, []string{"add", id, "--title", "-"}},
		{"step add with a negative after", Step, []string{"add", id, "--title", "-", "--after", "-1", "--agent", "hicks"}},
		{"step update summary from standard input, no agent", Step, []string{"update", id, "1", "--action", "finish", "--summary", "-"}},
		{"step plan from standard input, no agent", Step, []string{"plan", id, "--json-input"}},
		{"task start title from standard input, no agent", Task, []string{"start", "--title", "-", "--goal", "g"}},
		{"task close summary from standard input, no agent", Task, []string{"close", id, "--outcome", "done", "--summary", "-"}},
		{"task close json input, no agent", Task, []string{"close", id, "--outcome", "done", "--summary", "s", "--json-input"}},
		{"task update reason from standard input, no agent", Task, []string{"update", id, "--status", "blocked", "--reason", "-"}},
		{"task update json input, no agent", Task, []string{"update", id, "--json-input"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := &countingReader{r: strings.NewReader("input the command must not read\n")}
			opened := false
			open := func() (Session, error) {
				opened = true
				return Session{Svc: &fakeWrites{}, Ctx: core.WithCaller(context.Background(), testCaller)}, nil
			}
			var out, errOut bytes.Buffer
			code := c.cmd(open, c.args, in, &out, &errOut)
			if code != 2 || in.n != 0 || opened {
				t.Errorf("exit %d, read %d bytes of standard input, opened %v, stderr %q; want a usage error with no read and no open", code, in.n, opened, errOut.String())
			}
		})
	}
}

// TestInputsTextReadsStandardInputOnce checks that "-" reads the whole of standard input, drops
// one line break at its end, and that a second flag that asks for standard input is refused,
// naming the first.
func TestInputsTextReadsStandardInputOnce(t *testing.T) {
	in := &inputs{stdin: strings.NewReader("line one\nline two\n")}
	got, err := in.text("goal", "-")
	if err != nil || got != "line one\nline two" {
		t.Fatalf("text = %q, %v; want the input without its last line break", got, err)
	}
	if _, err := in.text("context", "-"); err == nil || !strings.Contains(err.Error(), "--goal and --context both read standard input") {
		t.Errorf("a second read of standard input: %v; want the refusal naming both flags", err)
	}
}

// TestInputsTextKeepsAValueThatIsNotDash checks that a value is used as it is written, and that
// standard input is not read for it.
func TestInputsTextKeepsAValueThatIsNotDash(t *testing.T) {
	in := &inputs{stdin: strings.NewReader("never read")}
	if got, err := in.text("goal", "-x"); err != nil || got != "-x" {
		t.Errorf("text = %q, %v; want -x", got, err)
	}
	if in.taken != "" {
		t.Errorf("standard input was taken by %q for a value that is not dash", in.taken)
	}
}

// TestInputsTextStopsAtTheCap checks that a text from standard input larger than the request
// cap is refused, not read whole.
func TestInputsTextStopsAtTheCap(t *testing.T) {
	in := &inputs{stdin: strings.NewReader(strings.Repeat("x", maxInputBytes+1))}
	if _, err := in.text("goal", "-"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("an input over the cap: %v; want the size refusal", err)
	}
}

// TestInputsDecodeRefusesAnInputOverTheCap checks that a --json-input larger than the request cap
// is refused as too large, not reported as bad JSON.
func TestInputsDecodeRefusesAnInputOverTheCap(t *testing.T) {
	var got struct {
		Criteria []string `json:"criteria"`
	}
	big := `{"criteria": ["` + strings.Repeat("x", maxInputBytes) + `"]}`
	in := &inputs{stdin: strings.NewReader(big)}
	if err := in.decode("json-input", &got); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("an input over the cap: %v; want the size refusal", err)
	}
}

// TestInputsDecodeRefusesAnUnknownKey checks that --json-input refuses a key the call does not
// name, so a misspelt field is not dropped, and that it reads one object.
func TestInputsDecodeRefusesAnUnknownKey(t *testing.T) {
	type shape struct {
		Criteria []string `json:"criteria"`
	}
	var got shape
	in := &inputs{stdin: strings.NewReader(`{"criteria": ["a"], "critera": ["b"]}`)}
	if err := in.decode("json-input", &got); err == nil || !strings.Contains(err.Error(), "not the JSON object this call takes") {
		t.Errorf("an unknown key: %v; want the refusal", err)
	}
}

// TestInputsDecodeRefusesTwoValues checks that a second JSON value after the object is refused.
func TestInputsDecodeRefusesTwoValues(t *testing.T) {
	var got core.TaskUpdateRequest
	in := &inputs{stdin: strings.NewReader(`{"id": "task-20261030-01"} {"id": "task-20261030-02"}`)}
	if err := in.decode("json-input", &got); err == nil || !strings.Contains(err.Error(), "more than one JSON value") {
		t.Errorf("two values: %v; want the refusal", err)
	}
}

// TestInputsDecodeReadsOneObject checks the form that is accepted: one object, with the keys it
// names.
func TestInputsDecodeReadsOneObject(t *testing.T) {
	var got struct {
		Criteria []string `json:"criteria"`
	}
	in := &inputs{stdin: strings.NewReader("{\"criteria\": [\"429 after 100 requests\"]}\n")}
	if err := in.decode("json-input", &got); err != nil || len(got.Criteria) != 1 {
		t.Errorf("decode = %+v, %v; want one criterion", got, err)
	}
}
