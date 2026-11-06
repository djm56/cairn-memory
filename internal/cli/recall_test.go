package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// fakeRecall is a core.Service whose recall answers what a test sets. It records the request of each
// call, and a test sets err to have every call refused.
type fakeRecall struct {
	core.Unimplemented
	err   error
	res   *core.RecallResult
	got   core.RecallRequest
	calls int
}

func (f *fakeRecall) Recall(_ context.Context, req core.RecallRequest) (*core.RecallResult, error) {
	f.calls++
	f.got = req
	return f.res, f.err
}

// recallRun runs Recall with svc behind it and stdin as its standard input.
func recallRun(svc core.Service, stdin io.Reader, args ...string) (int, string, string, int) {
	return counted(svc, func(open Opener, stdout, stderr io.Writer) int {
		return Recall(open, args, stdin, stdout, stderr)
	})
}

// sampleRecall is a recall as the server gives it: two directives from the sample list, one lesson,
// one mistake and one task, with the two counts set.
func sampleRecall() *core.RecallResult {
	return &core.RecallResult{
		Directives: sampleDirectives().Directives,
		Expired:    2,
		Omitted:    3,
		Lessons: []core.LessonHit{{
			TaskID: "task-20261030-01", Project: "acme-api",
			Lesson: core.Lesson{Kind: core.LessonReuse, Text: "Cache the lookup"},
		}},
		Mistakes: []core.MistakeHit{{
			TaskID: "task-20261030-01", Project: "acme-api",
			Mistake: core.Mistake{Agent: "hicks", Mistake: "Forgot the cap", Impact: core.ImpactHigh},
		}},
		Tasks: []core.TaskSummary{{ID: "task-20261030-01", Project: "acme-api", Title: "Add rate limit", Status: core.TaskActive}},
	}
}

// emptyRecall is a recall with nothing in any part. Each list is a non-nil empty slice, as the server
// gives it, so a test of the JSON form sees [] and not null.
func emptyRecall() *core.RecallResult {
	return &core.RecallResult{
		Directives: []core.Directive{}, Lessons: []core.LessonHit{}, Mistakes: []core.MistakeHit{}, Tasks: []core.TaskSummary{},
	}
}

// TestRecallHuman checks the summary of recall: the directives as directive list prints them, then one
// line each for the lesson, the mistake and the task, with the expired and omitted counts.
func TestRecallHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{res: sampleRecall()}
	code, out, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "add a rate limit")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	pad := strings.Repeat(" ", 10)
	want := strings.Join([]string{
		"  D-0002  agent hicks  Hicks reads first",
		pad + "Read the schema first.",
		"  D-0001  global  Global rule",
		pad + "Be exact.",
		"expired     2 past review_by, left out",
		"omitted     3 beyond the cap of 12",
		"lesson  task-20261030-01  acme-api  reuse: Cache the lookup",
		"mistake  task-20261030-01  acme-api  hicks  high: Forgot the cap",
		"task  task-20261030-01  acme-api  active  Add rate limit",
		"",
	}, "\n")
	if out != want {
		t.Errorf("human output = %q, want %q", out, want)
	}
}

// TestRecallEmptyHuman checks that a recall with nothing in any part says so, one empty-list line per
// part, in the same form the list verbs use.
func TestRecallEmptyHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{res: emptyRecall()}
	code, out, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "nothing matches")
	if code != 0 || errOut != "" || out != "no directives\nno lessons\nno mistakes\nno tasks\n" {
		t.Errorf("exit %d, stderr %q, output %q; want one empty-list line per part", code, errOut, out)
	}
}

// TestRecallJSON checks the --json output of recall: one line, the object the MCP tool returns, with
// every part and both counts.
func TestRecallJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{res: sampleRecall()}
	code, out, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "add a rate limit", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	var got core.RecallResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the output is not a recall result: %v\n%s", err, out)
	}
	if len(got.Directives) != 2 || got.Expired != 2 || got.Omitted != 3 ||
		len(got.Lessons) != 1 || got.Lessons[0].Project != "acme-api" || got.Lessons[0].Text != "Cache the lookup" ||
		len(got.Mistakes) != 1 || got.Mistakes[0].Mistake.Mistake != "Forgot the cap" ||
		len(got.Tasks) != 1 || got.Tasks[0].ID != "task-20261030-01" {
		t.Errorf("decoded result = %+v, want the parts and counts the server gave", got)
	}
}

// TestRecallEmptyJSON checks that each part of an empty recall is written as [], an array, and that
// the counts are 0. A script reading any part gets an array.
func TestRecallEmptyJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{res: emptyRecall()}
	code, out, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "nothing matches", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	for _, key := range []string{"directives", "lessons", "mistakes", "tasks"} {
		if got := jsonField(t, out, key); got != "[]" {
			t.Errorf("%s = %s, want []", key, got)
		}
	}
	if expired, omitted := jsonField(t, out, "expired"), jsonField(t, out, "omitted"); expired != "0" || omitted != "0" {
		t.Errorf("expired %s, omitted %s; want 0 and 0", expired, omitted)
	}
}

// TestRecallRequest checks the request recall sends: the goal and the limit as given, and the role
// named by --agent. Left out, the role is CAIRN_AGENT, and with neither there is no role.
func TestRecallRequest(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{res: emptyRecall()}
	if code, _, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "add a rate limit", "--limit", "3", "--agent", "reviewer"); code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if svc.got != (core.RecallRequest{Goal: "add a rate limit", Agent: "reviewer", Limit: 3}) {
		t.Errorf("the request = %+v, want the goal, the limit and the role as given", svc.got)
	}
	if code, _, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "g"); code != 0 || errOut != "" || svc.got.Agent != "" || svc.got.Limit != 0 {
		t.Errorf("exit %d, stderr %q, request %+v; want no role and no limit when neither is given", code, errOut, svc.got)
	}
}

// TestRecallRoleFromTheVariable checks the fallback: with --agent left out, CAIRN_AGENT names the
// caller's role, as task current reads it. A flag given wins over the variable.
func TestRecallRoleFromTheVariable(t *testing.T) {
	svc := &fakeRecall{res: emptyRecall()}
	t.Setenv(identity.EnvAgent, "hicks")
	if code, _, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "g"); code != 0 || errOut != "" || svc.got.Agent != "hicks" {
		t.Errorf("exit %d, stderr %q, role %q; want the role from %s", code, errOut, svc.got.Agent, identity.EnvAgent)
	}
	if code, _, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "g", "--agent", "reviewer"); code != 0 || errOut != "" || svc.got.Agent != "reviewer" {
		t.Errorf("exit %d, stderr %q, role %q; want the flag to win over the variable", code, errOut, svc.got.Agent)
	}
}

// TestRecallRefusedVariableIsAUsageError checks that a CAIRN_AGENT the wire form refuses is a usage
// error, named as coming from the variable, before any open or call.
func TestRecallRefusedVariableIsAUsageError(t *testing.T) {
	t.Setenv(identity.EnvAgent, "hicks ")
	svc := &fakeRecall{res: emptyRecall()}
	code, out, errOut, opened := recallRun(svc, strings.NewReader(""), "--goal", "g")
	if code != 2 || out != "" || !strings.Contains(errOut, "the name from "+identity.EnvAgent+" is refused") || opened != 0 || svc.calls != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d; want a usage error naming %s", code, out, errOut, opened, svc.calls, identity.EnvAgent)
	}
}

// TestRecallRefusedThroughTheFake checks that a refusal of the server exits 1 with the reason on
// stderr, prefixed with the command, and nothing on stdout.
func TestRecallRefusedThroughTheFake(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{err: core.Refused("the goal could not be read")}
	code, out, errOut, _ := recallRun(svc, strings.NewReader(""), "--goal", "g")
	if code != 5 || out != "" || errOut != "cairn recall: the goal could not be read\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 5 (refused) with the refusal on stderr", code, out, errOut)
	}
}

// TestRecallUsageErrors checks the usage errors of recall. Each exits 2 before the project is opened,
// makes no call, and reads no standard input.
func TestRecallUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no goal", []string{"--agent", "hicks"}, "--goal is required"},
		{"a positional argument", []string{"--goal", "g", "add a rate limit"}, "takes no positional argument"},
		{"a limit of zero", []string{"--goal", "g", "--limit", "0"}, "--limit must be 1 or more"},
		{"an agent with an edge space", []string{"--goal", "g", "--agent", "hicks "}, "is refused"},
		{"a bad limit with the goal from standard input", []string{"--goal", "-", "--limit", "0"}, "--limit must be 1 or more"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeRecall{res: emptyRecall()}
			stdin := &countingReader{r: strings.NewReader("never read")}
			code, out, errOut, opened := recallRun(svc, stdin, c.args...)
			if code != 2 || out != "" || !strings.Contains(errOut, c.want) || opened != 0 || svc.calls != 0 || stdin.n != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d, stdin bytes %d; want a usage error naming %q with no open, call or read",
					code, out, errOut, opened, svc.calls, stdin.n, c.want)
			}
		})
	}
}

// TestRecallGoalFromStandardInput checks that --goal - reads the goal from standard input, with the
// line break at the end dropped.
func TestRecallGoalFromStandardInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeRecall{res: emptyRecall()}
	if code, _, errOut, _ := recallRun(svc, strings.NewReader("fix the export\n"), "--goal", "-"); code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if svc.got.Goal != "fix the export" {
		t.Errorf("goal = %q, want the text read from standard input", svc.got.Goal)
	}
}

// TestRecallFoldsBidiCharacters checks that the human output holds no bidi override that a lesson
// carries. The character is built from its code point, so the test source holds none.
func TestRecallFoldsBidiCharacters(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	rlo := string(rune(0x202E))
	res := emptyRecall()
	res.Lessons = []core.LessonHit{{TaskID: "task-20261030-01", Project: "acme-api", Lesson: core.Lesson{Kind: core.LessonReuse, Text: "Cache" + rlo + " it"}}}
	svc := &fakeRecall{res: res}
	code, out, _, _ := recallRun(svc, strings.NewReader(""), "--goal", "g")
	if code != 0 || strings.Contains(out, rlo) {
		t.Errorf("exit %d, output %q: a bidi override reached the terminal", code, out)
	}
}
