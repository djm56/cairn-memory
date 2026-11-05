package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
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
