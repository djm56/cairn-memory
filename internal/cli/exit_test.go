package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// TestExitCodesOfTheTable checks the codes the table names: success, failure and usage are 0, 1 and 2, and
// each kind of the table has its own code from 3 to 8, in the order CONTRACT §1.2 lists them.
func TestExitCodesOfTheTable(t *testing.T) {
	if ExitOK != 0 || ExitFailure != 1 || ExitUsage != 2 {
		t.Errorf("ExitOK, ExitFailure, ExitUsage = %d, %d, %d; want 0, 1, 2", ExitOK, ExitFailure, ExitUsage)
	}
	want := []struct {
		kind core.Kind
		code int
	}{
		{core.KindInvalid, 3},
		{core.KindNotFound, 4},
		{core.KindRefused, 5},
		{core.KindOperatorOnly, 6},
		{api.KindNoRoute, 7},
		{api.KindUnsupportedMediaType, 8},
	}
	for _, w := range want {
		if got := ExitCodeFor(w.kind); got != w.code {
			t.Errorf("ExitCodeFor(%q) = %d, want %d", w.kind, got, w.code)
		}
	}
}

// TestExitCodeForAnyOtherKindIsOne checks that a failure with no kind of the table exits 1: an internal
// failure, a kind the client answers as a plain error (unauthenticated, read_only), and no kind at all.
func TestExitCodeForAnyOtherKindIsOne(t *testing.T) {
	for _, kind := range []core.Kind{api.KindInternal, api.KindUnauthenticated, api.KindReadOnly, ""} {
		if got := ExitCodeFor(kind); got != ExitFailure {
			t.Errorf("ExitCodeFor(%q) = %d, want %d", kind, got, ExitFailure)
		}
	}
}

// TestFailJSONFormIsTheAPIErrorBody checks the --json failure for each kind: one object on stdout, the error
// body the HTTP API answers with, its kind and message, nothing on stderr, and the kind's exit code.
func TestFailJSONFormIsTheAPIErrorBody(t *testing.T) {
	cases := []struct {
		err  error
		kind core.Kind
		code int
	}{
		{core.Invalid("the title is blank"), core.KindInvalid, 3},
		{core.NotFound("no task task-20261030-09 in this project"), core.KindNotFound, 4},
		{core.Refused("another task is active in this project"), core.KindRefused, 5},
		{core.OperatorOnly("this call is for the operator"), core.KindOperatorOnly, 6},
		{&core.Error{Kind: api.KindNoRoute, Message: "no call here"}, api.KindNoRoute, 7},
		{&core.Error{Kind: api.KindUnsupportedMediaType, Message: "not json"}, api.KindUnsupportedMediaType, 8},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		code := fail(&out, &errOut, "cairn task get", true, c.err)
		if code != c.code || errOut.Len() != 0 {
			t.Errorf("%v: exit %d, stderr %q; want exit %d and nothing on stderr", c.err, code, errOut.String(), c.code)
		}
		var got api.ErrorBody
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Error == nil {
			t.Fatalf("%v: stdout %q is not the error body (%v)", c.err, out.String(), err)
		}
		var want *core.Error
		if !errors.As(c.err, &want) {
			t.Fatalf("%v: the case is not a core error", c.err)
		}
		if got.Error.Kind != c.kind || got.Error.Message != want.Message {
			t.Errorf("%v: kind %q message %q, want kind %q message %q", c.err, got.Error.Kind, got.Error.Message, c.kind, want.Message)
		}
		if !strings.HasSuffix(out.String(), "\n") || strings.Count(out.String(), "\n") != 1 {
			t.Errorf("%v: stdout %q is not one line", c.err, out.String())
		}
	}
}

// TestFailJSONFormHasTheExactShape pins the bytes of one failure, so the field names cannot drift from the
// HTTP answer: {"error": {"kind", "message"}}, with missing left out when it is empty.
func TestFailJSONFormHasTheExactShape(t *testing.T) {
	var out, errOut bytes.Buffer
	code := fail(&out, &errOut, "cairn task get", true, core.NotFound("no task task-20261030-09 in this project"))
	const want = `{"error":{"kind":"not_found","message":"no task task-20261030-09 in this project"}}` + "\n"
	if code != 4 || out.String() != want {
		t.Errorf("exit %d, stdout %q; want exit 4 and %q", code, out.String(), want)
	}
}

// TestFailMissingListSurvivesTheTrip checks that the close policy's missing list is in the --json failure,
// under "missing", and reads back as the list core gave, through the client's wrapper as the server's answer
// reaches the CLI.
func TestFailMissingListSurvivesTheTrip(t *testing.T) {
	refusal := core.Refused("close policy not met: summary, lessons")
	refusal.Missing = []string{core.MissingSummary, core.MissingLessons}
	var out, errOut bytes.Buffer
	code := fail(&out, &errOut, "cairn task close", true, &client.ServerError{Err: refusal})
	if code != 5 {
		t.Errorf("exit %d, want 5", code)
	}
	if !strings.Contains(out.String(), `"missing":["summary","lessons"]`) {
		t.Errorf("stdout %q does not carry the missing list", out.String())
	}
	var got api.ErrorBody
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Error == nil {
		t.Fatalf("stdout %q is not the error body (%v)", out.String(), err)
	}
	if !slices.Equal(got.Error.Missing, refusal.Missing) {
		t.Errorf("missing = %v, want %v", got.Error.Missing, refusal.Missing)
	}
}

// TestFailTextFormIsOneLineOnStderr checks the failure without --json: one line on stderr with the command's
// name, as before, nothing on stdout, and the kind's exit code.
func TestFailTextFormIsOneLineOnStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	code := fail(&out, &errOut, "cairn step add", false, core.Refused("the task is done"))
	if code != 5 || out.Len() != 0 || errOut.String() != "cairn step add: the task is done\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 5, nothing on stdout and the one line", code, out.String(), errOut.String())
	}
}

// TestFailWithNoKindIsInternal checks a failure that is not a core error: it is kind internal, exit 1, and
// the --json form names that kind, as CONTRACT §1.2 says any other error is an internal failure.
func TestFailWithNoKindIsInternal(t *testing.T) {
	var out, errOut bytes.Buffer
	code := fail(&out, &errOut, "cairn task list", true, errors.New("the disk is full"))
	const want = `{"error":{"kind":"internal","message":"the disk is full"}}` + "\n"
	if code != ExitFailure || out.String() != want || errOut.Len() != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 and the internal error object", code, out.String(), errOut.String())
	}
}

// TestVerbFailureWithJSONExitsWithItsKind runs a verb end to end: a not_found from the server, with --json,
// writes the error object on stdout and exits 4.
func TestVerbFailureWithJSONExitsWithItsKind(t *testing.T) {
	svc := &fakeTasks{err: core.NotFound("no task task-20261030-09 in this project")}
	code, out, errOut := runTask(t, svc, "get", "task-20261030-09", "--json")
	if code != 4 || errOut != "" || out != `{"error":{"kind":"not_found","message":"no task task-20261030-09 in this project"}}`+"\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 4 and the not_found object", code, out, errOut)
	}
}

// TestVerbCloseRefusalCarriesTheMissingList runs task close end to end: a refused close with --json exits 5
// and its object names the missing list.
func TestVerbCloseRefusalCarriesTheMissingList(t *testing.T) {
	t.Setenv("CAIRN_AGENT", "")
	refusal := core.Refused("close policy not met: summary, lessons, mistakes")
	refusal.Missing = []string{core.MissingSummary, core.MissingLessons, core.MissingMistakes}
	svc := &fakeWrites{err: refusal}
	code, out, errOut := runWrite(t, Task, svc, "", "close", "task-20261030-01", "--outcome", "done", "--agent", "hicks", "--json")
	if code != 5 || errOut != "" || !strings.Contains(out, `"missing":["summary","lessons","mistakes"]`) {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 5, the missing list on stdout and nothing on stderr", code, out, errOut)
	}
}

// TestUsageErrorStaysExitTwoWithJSON checks that a usage error is not a server invalid: with --json it still
// exits 2, writes nothing to stdout, names the problem on stderr, and makes no call.
func TestUsageErrorStaysExitTwoWithJSON(t *testing.T) {
	svc := &fakeTasks{}
	code, out, errOut := runTask(t, svc, "get", "--json")
	if code != ExitUsage || out != "" || errOut == "" || svc.calls != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q, calls %d; want exit 2, nothing on stdout, a message and no call", code, out, errOut, svc.calls)
	}
}
