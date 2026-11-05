package main

import (
	"strings"
	"testing"
)

// TestSessionStartFoldsACodePointTitle checks, through the hook, a title with an embedded line feed and an
// escape sequence, both built from code points. The title ends no line of the block and starts none, and
// the escape sequence goes whole.
func TestSessionStartFoldsACodePointTitle(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	title := "Ship" + string(rune(0x0a)) + "now" + string(rune(0x1b)) + "[31m done"
	id := seedNamedTask(t, local, "acme-api", title, "Read the schema first")

	code, stdout, stderr := runCmd(t, "hook", "session-start")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := `Cairn task: "Ship now done" (` + id + `, active) in acme-api.` + "\n"
	if !strings.HasPrefix(stdout, want) {
		t.Errorf("session-start does not begin with the folded title line %q:\n%q", want, stdout)
	}
	if strings.ContainsRune(stdout, 0x1b) || strings.Contains(stdout, "Ship\n") {
		t.Errorf("session-start carries the line break or the escape:\n%q", stdout)
	}
}
