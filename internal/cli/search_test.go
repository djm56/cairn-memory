package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// fakeSearch is a core.Service whose search answers what a test sets. It records the request of each
// call, and a test sets err to have every call refused.
type fakeSearch struct {
	core.Unimplemented
	err   error
	res   *core.SearchResult
	got   core.SearchRequest
	calls int
}

func (f *fakeSearch) Search(_ context.Context, req core.SearchRequest) (*core.SearchResult, error) {
	f.calls++
	f.got = req
	return f.res, f.err
}

// searchRun runs Search with svc behind it and stdin as its standard input.
func searchRun(svc core.Service, stdin io.Reader, args ...string) (int, string, string, int) {
	return counted(svc, func(open Opener, stdout, stderr io.Writer) int {
		return Search(open, args, stdin, stdout, stderr)
	})
}

// sampleSearch is a search result as the server gives it: a task hit with its project, a directive hit
// with no project, and a cursor for the next page.
func sampleSearch() *core.SearchResult {
	return &core.SearchResult{
		Hits: []core.SearchHit{
			{Kind: core.HitTask, ID: "task-20261030-01", Project: "acme-api", TaskID: "task-20261030-01", Title: "Add rate limit", Snippet: "Add [rate] [limit] to the API"},
			{Kind: core.HitDirective, ID: "D-0002", Title: "Hicks reads first", Snippet: "Read the [schema] first."},
		},
		Next: "c2",
	}
}

// TestSearchHuman checks the summary of search: one line for each hit with its kind, id, project and
// title, its snippet under it, and the cursor of the next page.
func TestSearchHuman(t *testing.T) {
	svc := &fakeSearch{res: sampleSearch()}
	code, out, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "rate limit")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	pad := strings.Repeat(" ", 10)
	want := strings.Join([]string{
		"task  task-20261030-01  acme-api  Add rate limit",
		pad + "Add [rate] [limit] to the API",
		"directive  D-0002  Hicks reads first",
		pad + "Read the [schema] first.",
		"more hits: pass --cursor c2 to read the next page",
		"",
	}, "\n")
	if out != want {
		t.Errorf("human output = %q, want %q", out, want)
	}
}

// TestSearchEmptyHuman checks that a search with no hit says so, in the one empty-list form.
func TestSearchEmptyHuman(t *testing.T) {
	svc := &fakeSearch{res: &core.SearchResult{Hits: []core.SearchHit{}}}
	code, out, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "nothing matches")
	if code != 0 || errOut != "" || out != "no hits\n" {
		t.Errorf("exit %d, stderr %q, output %q; want the no-hit line", code, errOut, out)
	}
}

// TestSearchJSON checks the --json output of search: one line, the object the MCP tool returns, with
// the hits, each with its task id, and the next cursor.
func TestSearchJSON(t *testing.T) {
	svc := &fakeSearch{res: sampleSearch()}
	code, out, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "rate limit", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	var got core.SearchResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the output is not a search result: %v\n%s", err, out)
	}
	if len(got.Hits) != 2 || got.Hits[0].TaskID != "task-20261030-01" || got.Hits[1].ID != "D-0002" || got.Next != "c2" {
		t.Errorf("decoded result = %+v, want the hits and cursor the server gave", got)
	}
}

// TestSearchEmptyJSON checks that a search with no hit writes the hits as [], an array, and names no
// next cursor.
func TestSearchEmptyJSON(t *testing.T) {
	svc := &fakeSearch{res: &core.SearchResult{Hits: []core.SearchHit{}}}
	code, out, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "nothing matches", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	if got := jsonField(t, out, "hits"); got != "[]" {
		t.Errorf("hits = %s, want []", got)
	}
	if next := jsonField(t, out, "next"); next != "" {
		t.Errorf("next = %s with no page after it, want the key absent", next)
	}
}

// TestSearchRequest checks the request search sends: the query, each kind as it was given, the
// all-projects flag, the limit and the cursor.
func TestSearchRequest(t *testing.T) {
	svc := &fakeSearch{res: &core.SearchResult{Hits: []core.SearchHit{}}}
	if code, _, errOut, _ := searchRun(svc, strings.NewReader(""),
		"--query", "rate limit", "--kind", "lesson", "--kind", "mistake", "--all-projects", "--limit", "5", "--cursor", "c1"); code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	got := svc.got
	if got.Query != "rate limit" || fmt.Sprint(got.Kinds) != "[lesson mistake]" || !got.AllProjects || got.Limit != 5 || got.Cursor != "c1" {
		t.Errorf("the request = %+v, want the query, kinds, all-projects, limit and cursor as given", got)
	}
	if code, _, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "g"); code != 0 || errOut != "" || svc.got.AllProjects || svc.got.Kinds != nil {
		t.Errorf("exit %d, stderr %q, request %+v; want this project and every kind when neither is given", code, errOut, svc.got)
	}
}

// TestSearchAcceptsEveryKind checks that each of the seven kinds a search may name is accepted and
// sent, so the list checked here is the one the server takes.
func TestSearchAcceptsEveryKind(t *testing.T) {
	for _, k := range []string{"task", "event", "learning", "directive", "note", "lesson", "mistake"} {
		svc := &fakeSearch{res: &core.SearchResult{Hits: []core.SearchHit{}}}
		if code, _, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "g", "--kind", k); code != 0 || errOut != "" || fmt.Sprint(svc.got.Kinds) != "["+k+"]" {
			t.Errorf("--kind %s: exit %d, stderr %q, kinds %v; want the kind sent", k, code, errOut, svc.got.Kinds)
		}
	}
}

// TestSearchRefusedThroughTheFake checks that a refusal of the server exits 1 with the reason on
// stderr, prefixed with the command, and nothing on stdout.
func TestSearchRefusedThroughTheFake(t *testing.T) {
	svc := &fakeSearch{err: core.Refused("the cursor is from another search")}
	code, out, errOut, _ := searchRun(svc, strings.NewReader(""), "--query", "g", "--cursor", "x")
	if code != 1 || out != "" || errOut != "cairn search: the cursor is from another search\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 with the refusal on stderr", code, out, errOut)
	}
}

// TestSearchUsageErrors checks the usage errors of search. Each exits 2 before the project is opened,
// makes no call, and reads no standard input.
func TestSearchUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no query", []string{"--kind", "lesson"}, "--query is required"},
		{"a positional argument", []string{"--query", "g", "rate"}, "takes no positional argument"},
		{"a limit of zero", []string{"--query", "g", "--limit", "0"}, "--limit must be 1 or more"},
		{"an empty kind", []string{"--query", "g", "--kind", " "}, "a kind must be named"},
		{"an unknown kind", []string{"--query", "g", "--kind", "idea"}, "--kind must be task, event"},
		{"a learning kind, which is not a search kind", []string{"--query", "g", "--kind", "finding"}, "--kind must be task, event"},
		{"an unknown kind with the query from standard input", []string{"--query", "-", "--kind", "idea"}, "--kind must be task, event"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeSearch{res: &core.SearchResult{Hits: []core.SearchHit{}}}
			stdin := &countingReader{r: strings.NewReader("never read")}
			code, out, errOut, opened := searchRun(svc, stdin, c.args...)
			if code != 2 || out != "" || !strings.Contains(errOut, c.want) || opened != 0 || svc.calls != 0 || stdin.n != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d, stdin bytes %d; want a usage error naming %q with no open, call or read",
					code, out, errOut, opened, svc.calls, stdin.n, c.want)
			}
		})
	}
}

// TestSearchQueryFromStandardInput checks that --query - reads the query from standard input, with the
// line break at the end dropped.
func TestSearchQueryFromStandardInput(t *testing.T) {
	svc := &fakeSearch{res: &core.SearchResult{Hits: []core.SearchHit{}}}
	if code, _, errOut, _ := searchRun(svc, strings.NewReader("rate limit\n"), "--query", "-"); code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if svc.got.Query != "rate limit" {
		t.Errorf("query = %q, want the text read from standard input", svc.got.Query)
	}
}

// TestSearchFoldsBidiCharacters checks that the human output holds no bidi override that a hit's title
// carries. The character is built from its code point, so the test source holds none.
func TestSearchFoldsBidiCharacters(t *testing.T) {
	rlo := string(rune(0x202E))
	res := sampleSearch()
	res.Hits[0].Title = "Add" + rlo + " rate limit"
	svc := &fakeSearch{res: res}
	code, out, _, _ := searchRun(svc, strings.NewReader(""), "--query", "g")
	if code != 0 || strings.Contains(out, rlo) {
		t.Errorf("exit %d, output %q: a bidi override reached the terminal", code, out)
	}
}
