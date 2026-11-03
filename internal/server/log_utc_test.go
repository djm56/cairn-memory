package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestServerLogTimesAreUTC checks #137: a server log line's time is in UTC, whatever zone the
// time was taken in. The record is given a time five hours ahead of UTC, so a line that kept
// the host's offset would read +05:00 and fail. The record is built and handled directly, so no
// package or global state is changed, and the test is safe beside the server tests.
func TestServerLogTimesAreUTC(t *testing.T) {
	ahead := time.FixedZone("ahead", 5*60*60)
	at := time.Date(2026, 11, 3, 15, 10, 0, 0, ahead)

	var buf bytes.Buffer
	rec := slog.NewRecord(at, slog.LevelInfo, "listening", 0)
	rec.AddAttrs(slog.String("addr", "127.0.0.1:8788"))
	if err := newLogger(&buf).Handler().Handle(context.Background(), rec); err != nil {
		t.Fatalf("handle the record: %v", err)
	}
	line := buf.String()

	const key = "time="
	i := strings.Index(line, key)
	if i < 0 {
		t.Fatalf("no time field in the log line %q", line)
	}
	stamp := line[i+len(key):]
	if j := strings.IndexByte(stamp, ' '); j >= 0 {
		stamp = stamp[:j]
	}
	if want := "2026-11-03T10:10:00.000Z"; stamp != want {
		t.Errorf("log time %q, want %q: the time is in UTC, not the record's own offset", stamp, want)
	}
}
