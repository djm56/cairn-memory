package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// fakeLearning is a core.Service whose learning calls answer what a test sets. It records the
// context and the request of each call, and a test sets err to have every call refused.
type fakeLearning struct {
	core.Unimplemented
	err       error
	added     *core.Learning
	gotAddCtx context.Context
	gotAdd    core.LearningAddRequest
	list      *core.LearningList
	gotList   core.LearningListRequest
	calls     int
}

func (f *fakeLearning) LearningAdd(ctx context.Context, req core.LearningAddRequest) (*core.Learning, error) {
	f.calls++
	f.gotAddCtx, f.gotAdd = ctx, req
	return f.added, f.err
}

func (f *fakeLearning) LearningList(_ context.Context, req core.LearningListRequest) (*core.LearningList, error) {
	f.calls++
	f.gotList = req
	return f.list, f.err
}

// counted runs a command with svc behind an opener that counts how often it is called. It returns
// the exit code, stdout, stderr and the number of opens. A usage error must open nothing.
func counted(svc core.Service, run func(open Opener, stdout, stderr io.Writer) int) (int, string, string, int) {
	opened := 0
	open := func() (Session, error) {
		opened++
		return Session{Svc: svc, Ctx: core.WithCaller(context.Background(), testCaller)}, nil
	}
	var out, errOut bytes.Buffer
	code := run(open, &out, &errOut)
	return code, out.String(), errOut.String(), opened
}

// learningRun runs Learning with svc behind it and stdin as its standard input.
func learningRun(svc core.Service, stdin io.Reader, args ...string) (int, string, string, int) {
	return counted(svc, func(open Opener, stdout, stderr io.Writer) int {
		return Learning(open, args, stdin, stdout, stderr)
	})
}

// sampleLearning is a learning as the server returns it, with every field set.
func sampleLearning() core.Learning {
	return core.Learning{
		ID: 7, Project: "acme-api", TaskID: "task-20261105-01", Agent: "hicks",
		Kind: core.LearningFinding, Title: "Cache the lookup", Body: "The lookup runs twice.",
		Target: "internal/lookup.go", Rationale: "It doubles the cost", Example: "see the trace",
		Status: core.LearningProposed, CreatedAt: time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC),
	}
}

// TestLearningAddHuman checks the human output of learning add: every field the learning holds, one
// labelled line each, and the request carries each field and the agent from --agent.
func TestLearningAddHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	want := sampleLearning()
	svc := &fakeLearning{added: &want}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""),
		"add", "--kind", "finding", "--title", "Cache the lookup", "--body", "The lookup runs twice.",
		"--task-id", "task-20261105-01", "--target", "internal/lookup.go", "--rationale", "It doubles the cost",
		"--example", "see the trace", "--agent", "hicks")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, line := range []string{
		"learning    7  proposed  finding  Cache the lookup\n",
		fmt.Sprintf("%-11s %s\n", "project", "acme-api"),
		fmt.Sprintf("%-11s %s\n", "task", "task-20261105-01"),
		fmt.Sprintf("%-11s %s\n", "agent", "hicks"),
		fmt.Sprintf("%-11s %s\n", "target", "internal/lookup.go"),
		fmt.Sprintf("%-11s %s\n", "body", "The lookup runs twice."),
		fmt.Sprintf("%-11s %s\n", "rationale", "It doubles the cost"),
		fmt.Sprintf("%-11s %s\n", "example", "see the trace"),
		fmt.Sprintf("%-11s %s\n", "created", "2026-11-05 09:30 UTC"),
	} {
		if !strings.Contains(out, line) {
			t.Errorf("human output lacks the line %q:\n%s", line, out)
		}
	}
	if agentOf(t, svc.gotAddCtx) != "hicks" {
		t.Errorf("the call ran as %q, want hicks", agentOf(t, svc.gotAddCtx))
	}
	wantReq := core.LearningAddRequest{
		TaskID: "task-20261105-01", Kind: core.LearningFinding, Title: "Cache the lookup",
		Body: "The lookup runs twice.", Target: "internal/lookup.go", Rationale: "It doubles the cost", Example: "see the trace",
	}
	if svc.gotAdd != wantReq {
		t.Errorf("the request = %+v, want %+v", svc.gotAdd, wantReq)
	}
}

// TestLearningAddJSON checks the --json output of learning add: one line, the object the MCP tool
// returns, which decodes to the learning the server gave.
func TestLearningAddJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	want := sampleLearning()
	svc := &fakeLearning{added: &want}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""),
		"add", "--kind", "pattern", "--title", "t", "--body", "b", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	var got core.Learning
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the output is not a learning: %v\n%s", err, out)
	}
	if got.ID != 7 || got.Status != core.LearningProposed || got.Title != "Cache the lookup" {
		t.Errorf("decoded learning = %+v, want the one the server gave", got)
	}
}

// TestLearningAddRefusedThroughTheFake checks that a refusal of the server is printed on one line to
// stderr, with the command's name, and exits 1 with nothing on stdout.
func TestLearningAddRefusedThroughTheFake(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeLearning{err: core.NotFound("task task-20261105-99 not found")}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""),
		"add", "--kind", "finding", "--title", "t", "--body", "b", "--task-id", "task-20261105-99", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn learning add: task task-20261105-99 not found\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 with the refusal on stderr", code, out, errOut)
	}
}

// TestLearningAddUsageErrors checks the usage errors of learning add. Each exits 2, opens no project,
// makes no call and reads no standard input.
func TestLearningAddUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no kind", []string{"add", "--title", "t", "--body", "b", "--agent", "hicks"}, "--kind must be"},
		{"an unknown kind", []string{"add", "--kind", "idea", "--title", "t", "--body", "b", "--agent", "hicks"}, "--kind must be"},
		{"no title", []string{"add", "--kind", "finding", "--body", "b", "--agent", "hicks"}, "--title is required"},
		{"no body", []string{"add", "--kind", "finding", "--title", "t", "--agent", "hicks"}, "--body is required"},
		{"a positional argument", []string{"add", "task-20261105-01", "--kind", "finding", "--title", "t", "--body", "b", "--agent", "hicks"}, "takes no positional argument"},
		{"no agent", []string{"add", "--kind", "finding", "--title", "t", "--body", "b"}, "no agent name"},
		{"an agent with an edge space", []string{"add", "--kind", "finding", "--title", "t", "--body", "b", "--agent", "hicks "}, "is refused"},
		{"body from standard input and no body flag", []string{"add", "--kind", "finding", "--title", "-", "--agent", "hicks"}, "--body is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeLearning{}
			stdin := &countingReader{r: strings.NewReader("never read")}
			code, out, errOut, opened := learningRun(svc, stdin, c.args...)
			if code != 2 || out != "" || !strings.Contains(errOut, c.want) || opened != 0 || svc.calls != 0 || stdin.n != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d, stdin bytes %d; want a usage error naming %q with no open, call or read",
					code, out, errOut, opened, svc.calls, stdin.n, c.want)
			}
		})
	}
}

// TestLearningAddStandardInput checks that --body - reads the body from standard input: the line
// break at the end is dropped and the lines inside it are kept.
func TestLearningAddStandardInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	want := sampleLearning()
	svc := &fakeLearning{added: &want}
	code, _, errOut, _ := learningRun(svc, strings.NewReader("line one\nline two\n"),
		"add", "--kind", "finding", "--title", "t", "--body", "-", "--agent", "hicks")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if svc.gotAdd.Body != "line one\nline two" {
		t.Errorf("body = %q, want the two lines with the final break dropped", svc.gotAdd.Body)
	}
}

// TestLearningAddFoldsBidiCharacters checks that the human output holds no bidi override that a
// title carries. The character is built from its code point, so the test source holds none.
func TestLearningAddFoldsBidiCharacters(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	rlo := string(rune(0x202E))
	l := sampleLearning()
	l.Title = "Cache" + rlo + " the lookup"
	svc := &fakeLearning{added: &l}
	code, out, _, _ := learningRun(svc, strings.NewReader(""),
		"add", "--kind", "finding", "--title", "t", "--body", "b", "--agent", "hicks")
	if code != 0 || strings.Contains(out, rlo) {
		t.Errorf("exit %d, output %q: a bidi override reached the terminal", code, out)
	}
}

// TestLearningListHuman checks the summary of learning list: one line per learning with its id,
// status, kind, title and target, and the cursor of the next page. The filters are passed on as they
// were given, and a repeated --status or --kind lists each value.
func TestLearningListHuman(t *testing.T) {
	first := sampleLearning()
	second := sampleLearning()
	second.ID, second.Title, second.Target = 6, "Second", ""
	second.Status = core.LearningAccepted
	svc := &fakeLearning{list: &core.LearningList{Learnings: []core.Learning{first, second}, Next: "abc"}}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""),
		"list", "--status", "proposed", "--status", "accepted", "--kind", "finding", "--target", "internal/lookup.go", "--limit", "5", "--cursor", "c1")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	want := "7  proposed  finding  Cache the lookup  target internal/lookup.go\n" +
		"6  accepted  finding  Second\n" +
		"more learnings: pass --cursor abc to read the next page\n"
	if out != want {
		t.Errorf("human output = %q, want %q", out, want)
	}
	got := svc.gotList
	if fmt.Sprint(got.Status) != "[proposed accepted]" || fmt.Sprint(got.Kind) != "[finding]" ||
		got.Target != "internal/lookup.go" || got.Limit != 5 || got.Cursor != "c1" || got.AllProjects {
		t.Errorf("the request = %+v, want the filters as given and no all_projects", got)
	}
}

// TestLearningListEmpty checks that a list with no learning says so, in the one empty-list form.
func TestLearningListEmpty(t *testing.T) {
	svc := &fakeLearning{list: &core.LearningList{Learnings: []core.Learning{}}}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""), "list")
	if code != 0 || errOut != "" || out != "no learnings\n" {
		t.Errorf("exit %d, stderr %q, output %q; want the no-learning line", code, errOut, out)
	}
}

// TestLearningListCapsALongTarget checks that a target too long for one line is cut in the summary, as
// the title is, and that --json keeps it whole.
func TestLearningListCapsALongTarget(t *testing.T) {
	l := sampleLearning()
	l.Title, l.Target = "Long", strings.Repeat("x", 300)
	svc := &fakeLearning{list: &core.LearningList{Learnings: []core.Learning{l}}}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""), "list")
	want := "7  proposed  finding  Long  target " + strings.Repeat("x", MaxLineRunes-1) + "…\n"
	if code != 0 || errOut != "" || out != want {
		t.Errorf("exit %d, stderr %q, output %q; want the target cut to %d runes", code, errOut, out, MaxLineRunes)
	}
	code, out, _, _ = learningRun(svc, strings.NewReader(""), "list", "--json")
	if code != 0 || !strings.Contains(out, strings.Repeat("x", 300)) {
		t.Errorf("--json: exit %d; want the whole target", code)
	}
}

// TestLearningListJSON checks the --json output of learning list: one line, the object the MCP tool
// returns, with the learnings and the next cursor.
func TestLearningListJSON(t *testing.T) {
	svc := &fakeLearning{list: &core.LearningList{Learnings: []core.Learning{sampleLearning()}, Next: "abc"}}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""), "list", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	var got core.LearningList
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the output is not a learning list: %v\n%s", err, out)
	}
	if len(got.Learnings) != 1 || got.Learnings[0].ID != 7 || got.Next != "abc" {
		t.Errorf("decoded list = %+v, want the learning and the cursor the server gave", got)
	}
}

// TestLearningListEmptyJSON checks that a list with no learning writes the learnings as [], an array,
// and the next cursor as absent.
func TestLearningListEmptyJSON(t *testing.T) {
	svc := &fakeLearning{list: &core.LearningList{Learnings: []core.Learning{}}}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""), "list", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	if got := jsonField(t, out, "learnings"); got != "[]" {
		t.Errorf("learnings = %s, want []", got)
	}
	if next := jsonField(t, out, "next"); next != "" {
		t.Errorf("next = %s with no page after it, want the key absent", next)
	}
}

// TestLearningListRefusedThroughTheFake checks that a refusal of the server exits 1 with the reason
// on stderr and nothing on stdout.
func TestLearningListRefusedThroughTheFake(t *testing.T) {
	svc := &fakeLearning{err: core.Refused("the limit is over the page size")}
	code, out, errOut, _ := learningRun(svc, strings.NewReader(""), "list")
	if code != 1 || out != "" || errOut != "cairn learning list: the limit is over the page size\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 with the refusal on stderr", code, out, errOut)
	}
}

// TestLearningListUsageErrors checks the usage errors of learning list. Each exits 2 before the
// project is opened, and makes no call.
func TestLearningListUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"an empty status", []string{"list", "--status", " "}, "a status must be named"},
		{"an empty kind", []string{"list", "--kind", ""}, "a kind must be named"},
		{"an unknown kind", []string{"list", "--kind", "idea"}, "--kind must be finding, pattern or agent-note"},
		{"an unknown kind after a known one", []string{"list", "--kind", "finding", "--kind", "idea"}, "--kind must be finding, pattern or agent-note"},
		{"a limit of zero", []string{"list", "--limit", "0"}, "--limit must be 1 or more"},
		{"a positional argument", []string{"list", "task-20261105-01"}, "takes no positional argument"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeLearning{}
			code, out, errOut, opened := learningRun(svc, strings.NewReader(""), c.args...)
			if code != 2 || out != "" || !strings.Contains(errOut, c.want) || opened != 0 || svc.calls != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d; want a usage error naming %q with no open or call",
					code, out, errOut, opened, svc.calls, c.want)
			}
		})
	}
}

// TestLearningVerbsNeedAVerb checks that learning with no verb, or an unknown one, is a usage error
// that names the verbs.
func TestLearningVerbsNeedAVerb(t *testing.T) {
	svc := &fakeLearning{}
	if code, _, errOut, opened := learningRun(svc, strings.NewReader("")); code != 2 || !strings.Contains(errOut, "cairn learning add|list") || opened != 0 {
		t.Errorf("no verb: exit %d, stderr %q; want a usage error naming add|list", code, errOut)
	}
	if code, _, errOut, opened := learningRun(svc, strings.NewReader(""), "remove"); code != 2 || !strings.Contains(errOut, `unknown verb "remove"`) || opened != 0 {
		t.Errorf("unknown verb: exit %d, stderr %q; want a usage error", code, errOut)
	}
}
