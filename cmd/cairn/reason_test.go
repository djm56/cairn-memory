package main

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// TestReasonCapsOnlyServerText checks the split of #113. A message the server wrote is
// capped at maxLineRunes, with an ellipsis. A local error, a core.Error the client did not
// read from an answer or a file error with a long path, is printed whole, so the cause
// doctor shows is not cut. Both still lose their control characters.
func TestReasonCapsOnlyServerText(t *testing.T) {
	long := strings.Repeat("x", maxLineRunes+50)

	server := &client.ServerError{Err: &core.Error{Kind: core.KindInvalid, Message: long}}
	got := reason(server)
	if n := len([]rune(got)); n != maxLineRunes || !strings.HasSuffix(got, "…") {
		t.Errorf("a server message: %d runes, %q, want %d runes and an ellipsis", n, got, maxLineRunes)
	}

	local := &core.Error{Kind: core.KindInvalid, Message: long}
	if got := reason(local); got != long {
		t.Errorf("a local core.Error of %d runes was cut to %d", len(long), len([]rune(got)))
	}

	path := "/" + strings.Repeat("deep/", 60) + "cairn.toml"
	pathErr := &os.PathError{Op: "open", Path: path, Err: syscall.ENOENT}
	if got := reason(pathErr); !strings.Contains(got, path) {
		t.Errorf("a path error lost its path: %q", got)
	}

	if got := reason(&core.Error{Kind: core.KindInvalid, Message: "bad\x1b[2J text"}); got != "bad text" {
		t.Errorf("a local message kept its escape sequence: %q", got)
	}
	if got := reason(&client.ServerError{Err: &core.Error{Kind: core.KindInvalid, Message: "bad\x1b[2J text"}}); got != "bad text" {
		t.Errorf("a server message kept its escape sequence: %q", got)
	}
}
