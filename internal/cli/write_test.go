package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestAgentFlagBeatsTheVariable checks that --agent names the caller of a read when the
// variable is set too: the flag is what the person typed for this call.
func TestAgentFlagBeatsTheVariable(t *testing.T) {
	t.Setenv(EnvAgent, "hicks")
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
	t.Setenv(EnvAgent, "hicks")
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	if code, _, errOut := runTask(t, svc, "current"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if c, _ := core.CallerFrom(svc.gotCtx); c.Agent != "hicks" {
		t.Errorf("the caller agent is %q, want hicks from %s", c.Agent, EnvAgent)
	}
}

// TestBlankAgentIsNoAgentForARead checks that a name of only spaces is no name, and that a read
// still runs, with no agent, as CONTRACT §1.1 says.
func TestBlankAgentIsNoAgentForARead(t *testing.T) {
	t.Setenv(EnvAgent, "   ")
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
	t.Setenv(EnvAgent, "")
	fl := flag.NewFlagSet("cairn task start", flag.ContinueOnError)
	agent := fl.String("agent", "", "")
	if err := fl.Parse(nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if _, ok := agentName(fl, *agent, true, "cairn task start", &stderr); ok {
		t.Fatal("a write with no agent was accepted")
	}
	for _, want := range []string{"cairn task start: no agent name", "--agent", EnvAgent} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q lacks %q", stderr.String(), want)
		}
	}

	t.Setenv(EnvAgent, "reviewer")
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
