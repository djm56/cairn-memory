package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// useDir runs the commands from dir, with no caller set in the environment: the
// project and server come only from the files the test writes.
func useDir(t *testing.T, dir string) {
	t.Helper()
	assertNoAncestorProjectFile(t)
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	prev := workDir
	workDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { workDir = prev })
}

// assertNoAncestorProjectFile fails the test when a .cairn.toml sits above the temporary
// directory. A walk up from a test directory reaches the temporary directory's ancestors,
// so such a file would be read by the test and change what it sees (step 10 review S9).
// The test says so, rather than passing for the wrong reason.
func assertNoAncestorProjectFile(t *testing.T) {
	t.Helper()
	dir, err := filepath.Abs(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".cairn.toml")); err == nil {
			t.Fatalf("%s holds a .cairn.toml above the temporary directory, which the tests walk up through: move it, or set TMPDIR", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// runCmd runs one command line and returns its exit code and both streams.
func runCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// writeFile writes content to name under dir, and returns the path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// readBytes returns the bytes of path, failing the test when it cannot be read.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// destructive are the words no message may suggest (#70, CONTRACT §1.2). The audit
// reads the source; these tests read what a person is actually shown.
var destructive = []string{"--force", "rm ", "reset", "overwrite"}

// assertNoDestructive fails when any of the texts names a destructive step.
func assertNoDestructive(t *testing.T, texts ...string) {
	t.Helper()
	for _, text := range texts {
		for _, word := range destructive {
			if strings.Contains(text, word) {
				t.Errorf("output %q names %q, which is destructive advice", text, word)
			}
		}
	}
}

// newServer serves a real core.Local over the api handlers on a loopback httptest
// server, and returns the Local (to seed rows) and the server's URL. Every request
// is recorded in seen, with its project header, so a test can see what was sent.
func newServer(t *testing.T, seen *[]string) (*core.Local, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	local := core.NewLocal(st)
	handler := api.Handler(local, api.UnauthenticatedHeaderCaller, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = append(*seen, r.URL.Path+" project="+r.Header.Get(api.HeaderProject))
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return local, srv.URL
}

// seedTask starts an active task in project for the seeding of a server, and returns
// its id. It has two steps, the first of which is active.
func seedTask(t *testing.T, local *core.Local, project string) string {
	t.Helper()
	return seedNamedTask(t, local, project, "Wire the export", "Read the schema first")
}

// seedNamedTask is seedTask with the title and the next action given, so a test can
// put text in them that the server must carry as it is.
func seedNamedTask(t *testing.T, local *core.Local, project, title, nextAction string) string {
	t.Helper()
	ctx := core.WithCaller(context.Background(), core.Caller{Project: project, Agent: "main", Role: core.RoleAgent})
	res, err := local.TaskStart(ctx, core.TaskStartRequest{
		Title: title, Goal: "Print a task as Markdown.",
		Steps: []core.StepInput{{Title: "Read the schema"}, {Title: "Write the printer"}},
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	next := nextAction
	if _, err := local.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, NextAction: &next}); err != nil {
		t.Fatalf("seed next action: %v", err)
	}
	if _, err := local.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart}); err != nil {
		t.Fatalf("seed step start: %v", err)
	}
	return res.ID
}
