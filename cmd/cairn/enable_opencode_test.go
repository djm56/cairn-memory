package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// ocConfig and ocInstructions are the two files enable writes for OpenCode in the project at dir.
func ocConfig(dir string) string       { return filepath.Join(dir, openCodeFile) }
func ocInstructions(dir string) string { return filepath.Join(dir, ".cairn", "instructions.md") }

// ocBackup is the backup a run at enableClock makes of the opencode.json of the project called acme-api.
func ocBackup(t *testing.T) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "cairn", "backups", "acme-api", openCodeFile+"."+enableClock.Format(backupStamp))
}

// ocExisting is an opencode.json with keys and an MCP server that enable does not add to. Its layout is not the one enable
// writes, so a rewrite that changed a value would show.
const ocExisting = `{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-5-5",
  "mcp": {"docs": {"type": "remote", "url": "https://example.com/mcp"}},
  "instructions": ["CONTRIBUTING.md"]
}
`

// ocCairnEntry is the mcp entry enable writes for the cairn server, as decodeJSON reads it back.
func ocCairnEntry() map[string]any {
	return map[string]any{"type": "local", "command": []any{"cairn", "mcp"}, "timeout": json.Number("30000")}
}

// assertExperimental fails the test unless every line of out starts with the tag of agent, so a line copied out of a
// terminal still says the client is experimental.
func assertExperimental(t *testing.T, agent, out string) {
	t.Helper()
	if out == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !strings.HasPrefix(line, "EXPERIMENTAL "+agent+": ") {
			t.Errorf("line %q lacks the EXPERIMENTAL tag", line)
		}
	}
}

// TestOpenCodeWritesIntoAFreshProject checks a project with no OpenCode files: the config registers cairn and lists the
// instruction file, the instruction file holds the task loop with the OpenCode prefix, and nothing is backed up.
func TestOpenCodeWritesIntoAFreshProject(t *testing.T) {
	dir := enableProject(t)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := map[string]any{"mcp": map[string]any{"cairn": ocCairnEntry()}, "instructions": []any{openCodeInstructions}}
	if got := decodeJSON(t, readBytes(t, ocConfig(dir))); !reflect.DeepEqual(got, want) {
		t.Errorf("opencode.json holds %v, want %v", got, want)
	}
	text := string(readBytes(t, ocInstructions(dir)))
	if text != openCodeInstructionsText() {
		t.Errorf("instruction file is not the task loop with the OpenCode prefix:\n%s", text)
	}
	if !strings.Contains(text, "cairn_task_current") || strings.Contains(text, "{prefix}") {
		t.Errorf("instruction file does not name the tools with the prefix:\n%s", text)
	}
	for _, line := range []string{
		reportLine("mcp", "added", "cairn mcp in "+openCodeFile),
		reportLine("instructions", "added", openCodeInstructions+" in "+openCodeFile),
		reportLine("file", "added", openCodeInstructions),
		"wrote " + ocConfig(dir) + "\n",
		"wrote " + ocInstructions(dir) + "\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("stdout lacks %q:\n%s", line, stdout)
		}
	}
	assertExperimental(t, "opencode", stdout)
	if got := listDir(t, dir); !slices.Equal(got, []string{".cairn", ".cairn.toml", openCodeFile}) {
		t.Errorf("the project holds %v, want the config, the instruction directory and the project file", got)
	}
	if got := listDir(t, filepath.Join(dir, ".cairn")); !slices.Equal(got, []string{"instructions.md"}) {
		t.Errorf(".cairn holds %v, want the instruction file alone", got)
	}
}

// TestOpenCodeKeepsWhatIsThereAndBacksItUp checks an existing opencode.json: every key and server enable does not add to
// is kept with its value, the cairn server and the instruction path are added, and the original is copied to the backup
// first.
func TestOpenCodeKeepsWhatIsThereAndBacksItUp(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, ocConfig(dir), ocExisting)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	got := decodeJSON(t, readBytes(t, ocConfig(dir)))
	before := decodeJSON(t, []byte(ocExisting))
	for _, key := range []string{"$schema", "model"} {
		if !reflect.DeepEqual(got[key], before[key]) {
			t.Errorf("%s is %v, was %v", key, got[key], before[key])
		}
	}
	gotMCP := got["mcp"].(map[string]any)
	if !reflect.DeepEqual(gotMCP["docs"], before["mcp"].(map[string]any)["docs"]) {
		t.Errorf("the docs server is %v, was %v", gotMCP["docs"], before["mcp"].(map[string]any)["docs"])
	}
	if !reflect.DeepEqual(gotMCP["cairn"], ocCairnEntry()) {
		t.Errorf("the cairn server is %v, want %v", gotMCP["cairn"], ocCairnEntry())
	}
	if want := []any{"CONTRIBUTING.md", openCodeInstructions}; !reflect.DeepEqual(got["instructions"], want) {
		t.Errorf("instructions is %v, want %v", got["instructions"], want)
	}
	backup := ocBackup(t)
	if !bytes.Equal(readBytes(t, backup), []byte(ocExisting)) {
		t.Errorf("the backup is not the original file:\n%s", readBytes(t, backup))
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the backup is not 0600: %v, %v", info, err)
	}
	if !strings.Contains(stdout, "backup "+backup+"\n") {
		t.Errorf("stdout does not name the backup:\n%s", stdout)
	}
	if got := listDir(t, filepath.Dir(backup)); !slices.Equal(got, []string{filepath.Base(backup)}) {
		t.Errorf("the backup directory holds %v, want the one backup", got)
	}
	assertExperimental(t, "opencode", stdout)
}

// TestOpenCodeRunTwiceGivesTheSameFiles checks idempotence: a second run writes nothing, keeps the bytes the first run
// left, reports every part already present, and makes no second backup.
func TestOpenCodeRunTwiceGivesTheSameFiles(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, ocConfig(dir), ocExisting)
	if code, _, stderr := runCmd(t, "enable", "--agent", "opencode"); code != 0 {
		t.Fatalf("first run: exit %d, stderr %q", code, stderr)
	}
	firstConfig := readBytes(t, ocConfig(dir))
	firstInstructions := readBytes(t, ocInstructions(dir))

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("second run: exit %d, stderr %q", code, stderr)
	}
	if !bytes.Equal(readBytes(t, ocConfig(dir)), firstConfig) || !bytes.Equal(readBytes(t, ocInstructions(dir)), firstInstructions) {
		t.Errorf("the second run changed a file")
	}
	for _, line := range []string{
		"unchanged: " + ocConfig(dir) + "\n",
		"unchanged: " + ocInstructions(dir) + "\n",
		reportLine("mcp", "already present", "cairn mcp in "+openCodeFile),
		reportLine("instructions", "already present", openCodeInstructions+" in "+openCodeFile),
		reportLine("file", "already present", openCodeInstructions),
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("second run stdout lacks %q:\n%s", line, stdout)
		}
	}
	if got := listDir(t, filepath.Dir(ocBackup(t))); len(got) != 1 {
		t.Errorf("the backup directory holds %v, want the backup of the first run only", got)
	}
}

// TestOpenCodeLeavesADifferingCairnEntry checks a cairn server that is registered with other settings: it is reported
// as differing and left exactly as it was, and the rest of the file is still merged.
func TestOpenCodeLeavesADifferingCairnEntry(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, ocConfig(dir), `{"mcp": {"cairn": {"type": "local", "command": ["cairn", "serve"]}}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	got := decodeJSON(t, readBytes(t, ocConfig(dir)))["mcp"].(map[string]any)["cairn"]
	want := map[string]any{"type": "local", "command": []any{"cairn", "serve"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the cairn server is %v, want it left as %v", got, want)
	}
	if !strings.Contains(stdout, reportLine("mcp", "differs, left as is", "cairn mcp in "+openCodeFile)) {
		t.Errorf("stdout does not report the differing entry:\n%s", stdout)
	}
	if !strings.Contains(stdout, reportLine("instructions", "added", openCodeInstructions+" in "+openCodeFile)) {
		t.Errorf("the instruction list was not merged:\n%s", stdout)
	}
}

// TestOpenCodeRefusesWhatItCannotMerge checks the files enable refuses: JSON that is not valid (JSON with comments among
// them), a document or a section of the wrong shape. Each is refused with exit 1, nothing is printed on stdout, the file
// is left as it was, and no instruction file is written beside it.
func TestOpenCodeRefusesWhatItCannotMerge(t *testing.T) {
	cases := []struct {
		name    string
		content string
		reason  string
	}{
		{"not valid JSON", `{"mcp": `, "is not valid JSON"},
		{"JSON with comments", "{\n  // a comment\n}\n", "is not valid JSON"},
		{"a top level that is an array", `[]`, "is not a JSON object"},
		{"mcp that is an array", `{"mcp": []}`, `has a "mcp" key that is not a JSON object`},
		{"instructions that is a string", `{"instructions": "x.md"}`, `has a "instructions" key that is not an array of strings`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			writeSettings(t, ocConfig(dir), tc.content)

			code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
			if code != 1 || stdout != "" {
				t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
			}
			if !strings.Contains(stderr, ocConfig(dir)+" "+tc.reason) || !strings.Contains(stderr, "Nothing was changed") {
				t.Errorf("stderr %q does not name the file, the reason and that nothing was changed", stderr)
			}
			if string(readBytes(t, ocConfig(dir))) != tc.content {
				t.Errorf("the file changed:\n%s", readBytes(t, ocConfig(dir)))
			}
			if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", openCodeFile}) {
				t.Errorf("the project holds %v, want the file and nothing else", got)
			}
		})
	}
}

// ocInstructionsBackup is the backup a run at enableClock makes of the instruction file of the project called acme-api.
func ocInstructionsBackup(t *testing.T) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "cairn", "backups", "acme-api", "instructions.md."+enableClock.Format(backupStamp))
}

// TestOpenCodeLeavesAnInstructionFileWithoutTheMarker checks an instruction file that has no marker line: someone's own, so
// it is left as it is, with no backup, and the config is still merged to list it.
func TestOpenCodeLeavesAnInstructionFileWithoutTheMarker(t *testing.T) {
	dir := enableProject(t)
	const mine = "my own notes\n"
	writeSettings(t, ocInstructions(dir), mine)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if string(readBytes(t, ocInstructions(dir))) != mine {
		t.Errorf("the instruction file was written over:\n%s", readBytes(t, ocInstructions(dir)))
	}
	want := map[string]any{"mcp": map[string]any{"cairn": ocCairnEntry()}, "instructions": []any{openCodeInstructions}}
	if got := decodeJSON(t, readBytes(t, ocConfig(dir))); !reflect.DeepEqual(got, want) {
		t.Errorf("opencode.json holds %v, want the config still merged: %v", got, want)
	}
	if !strings.Contains(stdout, reportLine("file", "differs, left as is", openCodeInstructions)) {
		t.Errorf("stdout does not say the file was left as it is:\n%s", stdout)
	}
	if got := listDir(t, filepath.Dir(ocInstructionsBackup(t))); len(got) != 0 {
		t.Errorf("a backup was made of a file that was not changed: %v", got)
	}
}

// TestOpenCodeReplacesAnOlderCairnInstructionFile checks an instruction file with cairn's marker and older text: it is
// replaced, and the original is copied to the backup first.
func TestOpenCodeReplacesAnOlderCairnInstructionFile(t *testing.T) {
	dir := enableProject(t)
	older := instructionsMarker + "\nan older task loop\n"
	writeSettings(t, ocInstructions(dir), older)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if string(readBytes(t, ocInstructions(dir))) != openCodeInstructionsText() {
		t.Errorf("the instruction file is not the current task loop:\n%s", readBytes(t, ocInstructions(dir)))
	}
	backup := ocInstructionsBackup(t)
	if !bytes.Equal(readBytes(t, backup), []byte(older)) {
		t.Errorf("the backup is not the original file:\n%s", readBytes(t, backup))
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the backup is not 0600: %v, %v", info, err)
	}
	if !strings.Contains(stdout, reportLine("file", "replaced", openCodeInstructions)) || !strings.Contains(stdout, "backup "+backup+"\n") {
		t.Errorf("stdout does not report the replacement and its backup:\n%s", stdout)
	}
	assertExperimental(t, "opencode", stdout)
}

// TestOpenCodeCountsLineEndingsAsNoDifference checks an instruction file that is the current text with CRLF line endings,
// as a checkout with autocrlf writes it: it is already present, and it is not rewritten or backed up.
func TestOpenCodeCountsLineEndingsAsNoDifference(t *testing.T) {
	dir := enableProject(t)
	crlf := strings.ReplaceAll(openCodeInstructionsText(), "\n", "\r\n")
	writeSettings(t, ocInstructions(dir), crlf)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if string(readBytes(t, ocInstructions(dir))) != crlf {
		t.Errorf("the CRLF file was rewritten:\n%q", readBytes(t, ocInstructions(dir)))
	}
	if !strings.Contains(stdout, reportLine("file", "already present", openCodeInstructions)) {
		t.Errorf("stdout does not say the file is already present:\n%s", stdout)
	}
	if got := listDir(t, filepath.Dir(ocInstructionsBackup(t))); len(got) != 0 {
		t.Errorf("a backup was made of a file that did not change: %v", got)
	}
}

// TestOpenCodeRefusesASymlinkedConfig checks an opencode.json that is a symbolic link: the file it names is not touched.
func TestOpenCodeRefusesASymlinkedConfig(t *testing.T) {
	dir := enableProject(t)
	writeFile(t, dir, "elsewhere.json", ocExisting)
	if err := os.Symlink(filepath.Join(dir, "elsewhere.json"), ocConfig(dir)); err != nil {
		t.Skipf("this system cannot make a symbolic link: %v", err)
	}

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "symbolic link") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1 and the link named", code, stdout, stderr)
	}
	if string(readBytes(t, filepath.Join(dir, "elsewhere.json"))) != ocExisting {
		t.Errorf("the file the link names was changed")
	}
}

// TestOpenCodeDryRunWritesNothing checks --dry-run in both starting points: it names what would be written, and creates
// no file, directory or backup.
func TestOpenCodeDryRunWritesNothing(t *testing.T) {
	t.Run("a fresh project", func(t *testing.T) {
		dir := enableProject(t)

		code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode", "--dry-run")
		if code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
		}
		for _, line := range []string{"dry run: nothing written to " + ocConfig(dir) + "\n", "dry run: nothing written to " + ocInstructions(dir) + "\n"} {
			if !strings.Contains(stdout, line) {
				t.Errorf("stdout lacks %q:\n%s", line, stdout)
			}
		}
		if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml"}) {
			t.Errorf("the dry run left %v in the project", got)
		}
		assertExperimental(t, "opencode", stdout)
	})
	t.Run("an existing file", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, ocConfig(dir), ocExisting)

		if code, _, stderr := runCmd(t, "enable", "--agent", "opencode", "--dry-run"); code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
		}
		if string(readBytes(t, ocConfig(dir))) != ocExisting {
			t.Errorf("the dry run changed the file:\n%s", readBytes(t, ocConfig(dir)))
		}
		if got := listDir(t, filepath.Dir(ocBackup(t))); len(got) != 0 {
			t.Errorf("the dry run made a backup: %v", got)
		}
	})
}

// TestOpenCodeRefusesWhenNoCacheDirectoryIsKnown checks that an existing config is not changed when its backup has no
// place to go, and that the instruction file is not written either.
func TestOpenCodeRefusesWhenNoCacheDirectoryIsKnown(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, ocConfig(dir), ocExisting)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "set HOME or XDG_CACHE_HOME") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and the fix named", code, stdout, stderr)
	}
	if string(readBytes(t, ocConfig(dir))) != ocExisting {
		t.Errorf("the config changed:\n%s", readBytes(t, ocConfig(dir)))
	}
	if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", openCodeFile}) {
		t.Errorf("the project holds %v, want the config and nothing else", got)
	}
}

// TestOpenCodeFailureOnTheSecondFileNamesTheFirst checks a run that fails on its second write. The instruction file is
// written first, so it is in place when opencode.json is refused. stderr names it as written, says it had no original, and
// says opencode.json was not replaced, with its backup. Both ways the second write can fail are run: the file changes after
// it is read, and the rename fails.
func TestOpenCodeFailureOnTheSecondFileNamesTheFirst(t *testing.T) {
	t.Run("the config changes after it is read", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, ocConfig(dir), ocExisting)
		other := "{\"model\": \"other\"}\n"
		calls := 0
		onBeforeRename(t, func() {
			calls++
			if calls == 2 {
				if err := os.WriteFile(ocConfig(dir), []byte(other), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})

		code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
		}
		for _, want := range []string{
			ocConfig(dir) + " it changed after enable read it: this file was not replaced, and the original is kept at " + ocBackup(t),
			"already written, and not undone: " + ocInstructions(dir) + "; it did not exist before",
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, stderr)
			}
		}
		if strings.Contains(stderr, "nothing was replaced") {
			t.Errorf("stderr says nothing was replaced, which is false here:\n%s", stderr)
		}
		if string(readBytes(t, ocInstructions(dir))) != openCodeInstructionsText() {
			t.Errorf("the instruction file written first is not in place")
		}
		if string(readBytes(t, ocConfig(dir))) != other {
			t.Errorf("the config was replaced:\n%s", readBytes(t, ocConfig(dir)))
		}
	})
	t.Run("the rename fails", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, ocConfig(dir), ocExisting)
		calls := 0
		prev := renameFile
		renameFile = func(from, to string) error {
			calls++
			if calls == 2 {
				return errors.New("injected rename failure")
			}
			return os.Rename(from, to)
		}
		t.Cleanup(func() { renameFile = prev })

		code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
		}
		for _, want := range []string{
			"write " + ocConfig(dir) + ": injected rename failure; the file was not replaced",
			"already written, and not undone: " + ocInstructions(dir) + "; it did not exist before",
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, stderr)
			}
		}
		if string(readBytes(t, ocConfig(dir))) != ocExisting {
			t.Errorf("the config changed:\n%s", readBytes(t, ocConfig(dir)))
		}
	})
}

// TestInstructionsNameEveryTool checks the task loop names each tool cairn registers, with the client's prefix.
func TestInstructionsNameEveryTool(t *testing.T) {
	for _, tool := range []string{"task_current", "recall", "task_start", "step_update", "event_log", "learning_add", "task_close"} {
		if !strings.Contains(instructionsFor("mcp__cairn__"), "mcp__cairn__"+tool) {
			t.Errorf("the task loop does not name %s with the prefix", tool)
		}
	}
}
