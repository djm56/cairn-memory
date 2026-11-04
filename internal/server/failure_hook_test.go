package server

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFailureHookQuotesAndCapsThePath checks #160: the failure hook logs the route path quoted
// and cut to the UI's log cap, so a path with a line break cannot start a second log line, and a
// long path cannot grow the line without bound. The line break is the forged-line case.
func TestFailureHookQuotesAndCapsThePath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.URL.Path = "/api/v1/x\nlevel=INFO msg=forged " + strings.Repeat("y", 500)

	failureHook(logger)(req, errors.New("database is closed"))

	line := buf.String()
	if n := strings.Count(line, "\n"); n != 1 {
		t.Errorf("the hook wrote %d lines, want one: %q", n, line)
	}
	if !strings.Contains(line, `\n`) {
		t.Errorf("the line break in the path is not escaped: %q", line)
	}
	if !strings.Contains(line, "...") {
		t.Errorf("the long path is not cut and marked: %q", line)
	}
	if len(line) > 400 {
		t.Errorf("the log line is %d bytes, want the path capped: %q", len(line), line)
	}
	if !strings.Contains(line, "database is closed") {
		t.Errorf("the cause is missing from the line: %q", line)
	}
}
