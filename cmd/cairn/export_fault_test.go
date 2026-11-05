package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// errDiskFull is the error failingWriter returns. A test looks for its text on stderr.
var errDiskFull = errors.New("disk full")

// failingWriter is a stdout that refuses every write, as a full disk or a closed pipe does.
type failingWriter struct{}

// Write refuses the bytes and writes none of them.
func (failingWriter) Write([]byte) (int, error) { return 0, errDiskFull }

// TestExportTaskReportsAFailedWrite checks that a task export whose stdout write fails names the failure on
// stderr and exits 1, so a truncated document does not pass for a whole one.
func TestExportTaskReportsAFailedWrite(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedTask(t, local, "acme-api")

	var stderr bytes.Buffer
	code := runExportTask([]string{id}, failingWriter{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), errDiskFull.Error()) {
		t.Errorf("exit %d, stderr %q: want 1 and the write error on stderr", code, stderr.String())
	}
}

// TestExportProjectReportsAFailedWrite checks that a project export whose stdout write fails names the failure
// on stderr and exits 1.
func TestExportProjectReportsAFailedWrite(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	seedClosedTask(t, local, "acme-api", "First task")

	var stderr bytes.Buffer
	code := runExportProject(nil, failingWriter{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), errDiskFull.Error()) {
		t.Errorf("exit %d, stderr %q: want 1 and the write error on stderr", code, stderr.String())
	}
}

// newServerFailingOneTask serves a real core.Local, as newServer does, except that a task_get call whose body
// names the task seed returns is answered with a server error. seed runs before the server starts, so the
// test names the task by its id, with nothing shared with the handler, and it returns that id.
func newServerFailingOneTask(t *testing.T, seed func(local *core.Local) string) string {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	local := core.NewLocal(st)
	bad := seed(local)
	handler := api.Handler(local, api.UnauthenticatedHeaderCaller, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.Prefix+api.RouteTaskGet {
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte(bad)) {
				http.Error(w, "the test fails this read", http.StatusInternalServerError)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestExportProjectNamesTheTaskThatFailed checks that a read that fails partway through a project export names
// the task it failed on, exits 1, and prints nothing: the export is all or nothing.
func TestExportProjectNamesTheTaskThatFailed(t *testing.T) {
	var bad string
	url := newServerFailingOneTask(t, func(local *core.Local) string {
		seedClosedTask(t, local, "acme-api", "First task")
		bad = seedClosedTask(t, local, "acme-api", "Second task")
		return bad
	})
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, stderr := runCmd(t, "export", "project")
	if code != 1 || stdout != "" {
		t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
	}
	if !strings.HasPrefix(stderr, "cairn export project: task "+bad+": ") {
		t.Errorf("stderr %q does not name task %s, the one that failed", stderr, bad)
	}
}

// TestExportProjectOfAnEmptyProjectReportsAFailedWrite checks that the one line an empty project prints is
// written through the same check: a failed write exits 1 and says so.
func TestExportProjectOfAnEmptyProjectReportsAFailedWrite(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	var stderr bytes.Buffer
	code := runExportProject(nil, failingWriter{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), errDiskFull.Error()) {
		t.Errorf("exit %d, stderr %q: want 1 and the write error on stderr", code, stderr.String())
	}
}
