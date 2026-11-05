package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// piConfigFile and piAgentsFile are the two files enable writes for pi in the project at dir.
func piConfigFile(dir string) string { return filepath.Join(dir, ".pi", "mcp.json") }
func piAgentsFile(dir string) string { return filepath.Join(dir, "AGENTS.md") }

// piBackupOf is the backup a run at enableClock makes of the file called name in the project called acme-api.
func piBackupOf(t *testing.T, name string) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "cairn", "backups", "acme-api", name+"."+enableClock.Format(backupStamp))
}

// piEntryDecoded is the mcpServers entry enable writes for cairn, as decodeJSON reads it back.
func piEntryDecoded() map[string]any {
	return map[string]any{"command": "cairn", "args": []any{"mcp"}, "exposure": "direct"}
}

// piExisting is a .pi/mcp.json with a server and a key that enable does not add to.
const piExisting = `{
  "mcpServers": {"docs": {"url": "https://example.com/mcp"}},
  "autoEnableCodemode": false
}
`

// piNotes is an AGENTS.md with text of the project's own, which enable must keep as it is.
const piNotes = "# Notes\n\nKeep the tests green.\n"

// TestPiWritesIntoAFreshProject checks a project with no pi files: the project file registers cairn with exposure direct,
// and AGENTS.md is written with the block only, the tool names carrying pi's prefix.
func TestPiWritesIntoAFreshProject(t *testing.T) {
	dir := enableProject(t)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := map[string]any{"mcpServers": map[string]any{"cairn": piEntryDecoded()}}
	if got := decodeJSON(t, readBytes(t, piConfigFile(dir))); !reflect.DeepEqual(got, want) {
		t.Errorf(".pi/mcp.json holds %v, want %v", got, want)
	}
	agents := string(readBytes(t, piAgentsFile(dir)))
	if agents != piBlock() {
		t.Errorf("AGENTS.md is not the block alone:\n%s", agents)
	}
	if !strings.Contains(agents, "mcp__cairn__task_current") || strings.Contains(agents, "{prefix}") {
		t.Errorf("AGENTS.md does not name the tools with pi's prefix:\n%s", agents)
	}
	for _, line := range []string{
		reportLine("mcp", "added", piConfig+" mcpServers.cairn"),
		reportLine("instructions", "added", piAgents+" block"),
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("stdout lacks %q:\n%s", line, stdout)
		}
	}
	assertExperimental(t, "pi", stdout)
	if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", ".pi", "AGENTS.md"}) {
		t.Errorf("the project holds %v, want the project file, .pi and AGENTS.md", got)
	}
}

// TestPiKeepsWhatIsThereAndBacksItUp checks an existing project file and AGENTS.md: every key and server enable does not
// add to is kept, the block is added after the text of AGENTS.md, and each original is copied to the backup first.
func TestPiKeepsWhatIsThereAndBacksItUp(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, piConfigFile(dir), piExisting)
	writeSettings(t, piAgentsFile(dir), piNotes)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	got := decodeJSON(t, readBytes(t, piConfigFile(dir)))
	if got["autoEnableCodemode"] != false {
		t.Errorf("autoEnableCodemode is %v, want false", got["autoEnableCodemode"])
	}
	servers := got["mcpServers"].(map[string]any)
	if want := map[string]any{"url": "https://example.com/mcp"}; !reflect.DeepEqual(servers["docs"], want) {
		t.Errorf("the docs server is %v, want %v", servers["docs"], want)
	}
	if !reflect.DeepEqual(servers["cairn"], piEntryDecoded()) {
		t.Errorf("the cairn server is %v, want %v", servers["cairn"], piEntryDecoded())
	}
	if got := string(readBytes(t, piAgentsFile(dir))); got != piNotes+"\n"+piBlock() {
		t.Errorf("AGENTS.md is not the notes, a blank line and the block:\n%s", got)
	}
	for _, p := range []struct{ file, original string }{{piConfigFile(dir), piExisting}, {piAgentsFile(dir), piNotes}} {
		backup := piBackupOf(t, filepath.Base(p.file))
		if !bytes.Equal(readBytes(t, backup), []byte(p.original)) {
			t.Errorf("the backup of %s is not the original:\n%s", p.file, readBytes(t, backup))
		}
		if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the backup of %s is not 0600: %v, %v", p.file, info, err)
		}
		if !strings.Contains(stdout, "backup "+backup+"\n") {
			t.Errorf("stdout does not name the backup of %s:\n%s", p.file, stdout)
		}
	}
	assertExperimental(t, "pi", stdout)
}

// TestPiLeavesAWholeBlockAlone checks an AGENTS.md that already holds the block: the file is not written, nothing is backed
// up for it, and the text on either side of the block stays.
func TestPiLeavesAWholeBlockAlone(t *testing.T) {
	dir := enableProject(t)
	existing := piNotes + "\n" + piBlock() + "More notes after the block.\n"
	writeSettings(t, piAgentsFile(dir), existing)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if string(readBytes(t, piAgentsFile(dir))) != existing {
		t.Errorf("AGENTS.md changed:\n%s", readBytes(t, piAgentsFile(dir)))
	}
	for _, line := range []string{
		"unchanged: " + piConfigFile(dir) + "\n",
		"unchanged: " + piAgentsFile(dir) + "\n",
		reportLine("mcp", "already present", piConfig+" mcpServers.cairn"),
		reportLine("instructions", "already present", piAgents+" block"),
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("stdout lacks %q:\n%s", line, stdout)
		}
	}
	if got := listDir(t, filepath.Dir(piBackupOf(t, "AGENTS.md"))); len(got) != 0 {
		t.Errorf("a backup was made of a file that did not change: %v", got)
	}
}

// TestPiLeavesACRLFBlockAlone checks the CRLF twin of TestPiLeavesAWholeBlockAlone: AGENTS.md with the current block and CRLF
// line endings, as a checkout with autocrlf gives it, is the same text. It is not written, and no backup is made.
func TestPiLeavesACRLFBlockAlone(t *testing.T) {
	dir := enableProject(t)
	existing := strings.ReplaceAll(piNotes+"\n"+piBlock()+"More notes after the block.\n", "\n", "\r\n")
	writeSettings(t, piAgentsFile(dir), existing)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if string(readBytes(t, piAgentsFile(dir))) != existing {
		t.Errorf("AGENTS.md with CRLF line endings changed:\n%q", readBytes(t, piAgentsFile(dir)))
	}
	if !strings.Contains(stdout, "unchanged: "+piAgentsFile(dir)+"\n") || !strings.Contains(stdout, reportLine("instructions", "already present", piAgents+" block")) {
		t.Errorf("stdout does not say the CRLF block is already present:\n%s", stdout)
	}
	if got := listDir(t, filepath.Dir(piBackupOf(t, "AGENTS.md"))); len(got) != 0 {
		t.Errorf("a backup was made of a CRLF file that did not change: %v", got)
	}
}

// TestPiUpdatesACRLFBlockAndKeepsItsLineEndings checks an older cairn block in a CRLF file: the block is replaced by the current
// text with CRLF line endings, the CRLF text on either side is kept byte for byte, and the original is copied to the backup.
func TestPiUpdatesACRLFBlockAndKeepsItsLineEndings(t *testing.T) {
	dir := enableProject(t)
	older := strings.ReplaceAll(piBeginLine+"\nan older task loop\n"+piEnd+"\n", "\n", "\r\n")
	original := strings.ReplaceAll(piNotes, "\n", "\r\n") + "\r\n" + older + strings.ReplaceAll("More notes after the block.\n", "\n", "\r\n")
	writeSettings(t, piAgentsFile(dir), original)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}}`)

	code, _, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := strings.ReplaceAll(piNotes+"\n"+strings.TrimSuffix(piBlock(), "\n")+"\nMore notes after the block.\n", "\n", "\r\n")
	if got := string(readBytes(t, piAgentsFile(dir))); got != want {
		t.Errorf("AGENTS.md is not the CRLF notes, the current block in CRLF and the notes after it:\n%q", got)
	}
	if !bytes.Equal(readBytes(t, piBackupOf(t, "AGENTS.md")), []byte(original)) {
		t.Errorf("the backup is not the original CRLF file:\n%q", readBytes(t, piBackupOf(t, "AGENTS.md")))
	}
}

// TestPiRunTwiceGivesTheSameFiles checks idempotence from a project with the files already there: a second run writes
// nothing and makes no second backup.
func TestPiRunTwiceGivesTheSameFiles(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, piConfigFile(dir), piExisting)
	writeSettings(t, piAgentsFile(dir), piNotes)
	if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 {
		t.Fatalf("first run: exit %d, stderr %q", code, stderr)
	}
	firstConfig := readBytes(t, piConfigFile(dir))
	firstAgents := readBytes(t, piAgentsFile(dir))
	backups := listDir(t, filepath.Dir(piBackupOf(t, "AGENTS.md")))

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("second run: exit %d, stderr %q", code, stderr)
	}
	if !bytes.Equal(readBytes(t, piConfigFile(dir)), firstConfig) || !bytes.Equal(readBytes(t, piAgentsFile(dir)), firstAgents) {
		t.Errorf("the second run changed a file")
	}
	if got := listDir(t, filepath.Dir(piBackupOf(t, "AGENTS.md"))); !slices.Equal(got, backups) {
		t.Errorf("the backups are %v after the second run, were %v", got, backups)
	}
	if !strings.Contains(stdout, "unchanged: ") {
		t.Errorf("stdout does not say nothing changed:\n%s", stdout)
	}
}

// TestPiLeavesADifferingCairnServer checks a cairn server in .pi/mcp.json with other settings: it is reported and left.
func TestPiLeavesADifferingCairnServer(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["serve"]}}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := map[string]any{"command": "cairn", "args": []any{"serve"}}
	if got := decodeJSON(t, readBytes(t, piConfigFile(dir)))["mcpServers"].(map[string]any)["cairn"]; !reflect.DeepEqual(got, want) {
		t.Errorf("the cairn server is %v, want it left as %v", got, want)
	}
	if !strings.Contains(stdout, reportLine("mcp", "differs, left as is", piConfig+" mcpServers.cairn")) {
		t.Errorf("stdout does not report the differing server:\n%s", stdout)
	}
}

// TestPiRefusesWhatItCannotMerge checks each file enable refuses. A refusal of either file stops the run before anything is
// written, so the other file is not created either.
func TestPiRefusesWhatItCannotMerge(t *testing.T) {
	cases := []struct {
		name     string
		config   string // the .pi/mcp.json to write, or "" for none
		agents   string // the AGENTS.md to write, or "" for none
		wantFile string // the file the refusal names
		reason   string
	}{
		{"a project file that is not plain JSON", `{"mcpServers": `, "", "config", "is not plain JSON (comments or a syntax error)"},
		{"a project file that is an array", `[]`, "", "config", "is not a JSON object"},
		{"mcpServers that is an array", `{"mcpServers": []}`, "", "config", `has a "mcpServers" key that is not a JSON object`},
		{"a start marker without its end", "", "# Notes\n" + piBegin + " -->\n", "agents", "has a cairn block that is not whole"},
		{"a doubled block", "", piBlock() + piBlock(), "agents", "has a cairn block that is not whole"},
		{"an end before its start", "", piEnd + "\n" + piBegin + " -->\n", "agents", "has a cairn block that is not whole"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			if tc.config != "" {
				writeSettings(t, piConfigFile(dir), tc.config)
			}
			if tc.agents != "" {
				writeSettings(t, piAgentsFile(dir), tc.agents)
			}
			path := piConfigFile(dir)
			if tc.wantFile == "agents" {
				path = piAgentsFile(dir)
			}

			code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
			if code != 1 || stdout != "" {
				t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
			}
			if !strings.Contains(stderr, path+" "+tc.reason) || !strings.Contains(stderr, "Nothing was changed") {
				t.Errorf("stderr %q does not name the file, the reason and that nothing was changed", stderr)
			}
			if tc.config != "" && string(readBytes(t, piConfigFile(dir))) != tc.config {
				t.Errorf(".pi/mcp.json changed")
			}
			if tc.agents != "" && string(readBytes(t, piAgentsFile(dir))) != tc.agents {
				t.Errorf("AGENTS.md changed")
			}
			if tc.config == "" {
				if _, err := os.Stat(piConfigFile(dir)); !os.IsNotExist(err) {
					t.Errorf(".pi/mcp.json was written beside the refused file (stat error %v)", err)
				}
			}
		})
	}
}

// TestPiAppendsToAnAgentsFileWithoutItsLineBreak checks the separator branch: AGENTS.md text that does not end in a line break
// gets one, then a blank line, then the block. An empty AGENTS.md gets the block alone.
func TestPiAppendsToAnAgentsFileWithoutItsLineBreak(t *testing.T) {
	t.Run("text with no line break at the end", func(t *testing.T) {
		dir := enableProject(t)
		const notes = "# Notes\n\nKeep the tests green."
		writeSettings(t, piAgentsFile(dir), notes)

		if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if got := string(readBytes(t, piAgentsFile(dir))); got != notes+"\n\n"+piBlock() {
			t.Errorf("AGENTS.md is not the notes, a line break, a blank line and the block:\n%q", got)
		}
		if !bytes.Equal(readBytes(t, piBackupOf(t, "AGENTS.md")), []byte(notes)) {
			t.Errorf("the backup is not the original notes")
		}
	})
	t.Run("an empty file", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, piAgentsFile(dir), "")

		if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if got := string(readBytes(t, piAgentsFile(dir))); got != piBlock() {
			t.Errorf("AGENTS.md is not the block alone:\n%q", got)
		}
	})
}

// TestPiRefusesPathsItCannotWriteThrough checks a path enable writes that holds something else: a directory where
// .pi/mcp.json or AGENTS.md goes, and a .pi that is a file. Each is refused while the run plans its files, so exit 1 comes
// with nothing written, and the refusal names the path.
func TestPiRefusesPathsItCannotWriteThrough(t *testing.T) {
	mkdir := func(t *testing.T, path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  func(dir string) string // the text stderr must hold
		kept  []string                // what the project holds after the refusal
	}{
		{
			name:  "a directory named mcp.json",
			setup: func(t *testing.T, dir string) { mkdir(t, piConfigFile(dir)) },
			want:  func(dir string) string { return piConfigFile(dir) + " is not a regular file" },
			kept:  []string{".cairn.toml", ".pi"},
		},
		{
			name:  "a directory named AGENTS.md",
			setup: func(t *testing.T, dir string) { mkdir(t, piAgentsFile(dir)) },
			want:  func(dir string) string { return piAgentsFile(dir) + " is not a regular file" },
			kept:  []string{".cairn.toml", piAgents},
		},
		{
			name:  "a .pi that is a file",
			setup: func(t *testing.T, dir string) { writeFile(t, dir, ".pi", "not a directory\n") },
			want:  func(dir string) string { return "look for " + piConfigFile(dir) },
			kept:  []string{".cairn.toml", ".pi"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			tc.setup(t, dir)
			want := tc.want(dir)

			code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
			if code != 1 || stdout != "" || !strings.Contains(stderr, want) {
				t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and %q on stderr", code, stdout, stderr, want)
			}
			if got := listDir(t, dir); !slices.Equal(got, tc.kept) {
				t.Errorf("the project holds %v, want %v", got, tc.kept)
			}
		})
	}
}

// TestPiRefusesADotPiThatLeavesTheProject checks a .pi that is a link to a directory outside the project: enable would write
// the registration into that directory, so it refuses while it plans, with exit 1, nothing written outside the project and
// nothing written inside it. A .pi that is a dangling link is refused too, since enable cannot tell where it would write.
func TestPiRefusesADotPiThatLeavesTheProject(t *testing.T) {
	t.Run("a link to a directory outside", func(t *testing.T) {
		dir := enableProject(t)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(dir, ".pi")); err != nil {
			t.Skipf("this system cannot make a symbolic link: %v", err)
		}

		code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
		if code != 1 || stdout != "" || !strings.Contains(stderr, "leads outside the project") {
			t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and the link named", code, stdout, stderr)
		}
		if got := listDir(t, outside); len(got) != 0 {
			t.Errorf("a file was written outside the project: %v", got)
		}
		if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", ".pi"}) {
			t.Errorf("the project holds %v, want the project file and the link alone", got)
		}
	})
	t.Run("a dangling link", func(t *testing.T) {
		dir := enableProject(t)
		if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, ".pi")); err != nil {
			t.Skipf("this system cannot make a symbolic link: %v", err)
		}

		code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
		if code != 1 || stdout != "" || !strings.Contains(stderr, "cannot be resolved") {
			t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and the link refused", code, stdout, stderr)
		}
		if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", ".pi"}) {
			t.Errorf("the project holds %v, want the project file and the link alone", got)
		}
	})
}

// TestPiRefusesASymlinkedAgentsFile checks an AGENTS.md that is a symbolic link: the file it names is not touched.
func TestPiRefusesASymlinkedAgentsFile(t *testing.T) {
	dir := enableProject(t)
	writeFile(t, dir, "elsewhere.md", piNotes)
	if err := os.Symlink(filepath.Join(dir, "elsewhere.md"), piAgentsFile(dir)); err != nil {
		t.Skipf("this system cannot make a symbolic link: %v", err)
	}

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "symbolic link") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1 and the link named", code, stdout, stderr)
	}
	if string(readBytes(t, filepath.Join(dir, "elsewhere.md"))) != piNotes {
		t.Errorf("the file the link names was changed")
	}
}

// TestPiDryRunWritesNothing checks --dry-run in both starting points: it names what would be written, and creates no file,
// directory or backup.
func TestPiDryRunWritesNothing(t *testing.T) {
	t.Run("a fresh project", func(t *testing.T) {
		dir := enableProject(t)

		code, stdout, stderr := runCmd(t, "enable", "--agent", "pi", "--dry-run")
		if code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
		}
		for _, line := range []string{"dry run: nothing written to " + piConfigFile(dir) + "\n", "dry run: nothing written to " + piAgentsFile(dir) + "\n"} {
			if !strings.Contains(stdout, line) {
				t.Errorf("stdout lacks %q:\n%s", line, stdout)
			}
		}
		if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml"}) {
			t.Errorf("the dry run left %v in the project", got)
		}
		assertExperimental(t, "pi", stdout)
	})
	t.Run("existing files", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, piConfigFile(dir), piExisting)
		writeSettings(t, piAgentsFile(dir), piNotes)

		if code, _, stderr := runCmd(t, "enable", "--agent", "pi", "--dry-run"); code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
		}
		if string(readBytes(t, piConfigFile(dir))) != piExisting || string(readBytes(t, piAgentsFile(dir))) != piNotes {
			t.Errorf("the dry run changed a file")
		}
		if got := listDir(t, filepath.Dir(piBackupOf(t, "AGENTS.md"))); len(got) != 0 {
			t.Errorf("the dry run made a backup: %v", got)
		}
	})
}

// TestPiRefusesWhenNoCacheDirectoryIsKnown checks that existing files are not changed when their backups have no place to
// go, and that the other file is not written either.
func TestPiRefusesWhenNoCacheDirectoryIsKnown(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, piAgentsFile(dir), piNotes)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "set HOME or XDG_CACHE_HOME") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and the fix named", code, stdout, stderr)
	}
	if string(readBytes(t, piAgentsFile(dir))) != piNotes {
		t.Errorf("AGENTS.md changed:\n%s", readBytes(t, piAgentsFile(dir)))
	}
	if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", "AGENTS.md"}) {
		t.Errorf("the project holds %v, want AGENTS.md and the project file alone", got)
	}
}

// TestPiFailureOnTheSecondFileNamesTheFirst checks a run that fails on its second write. The project file is written first,
// so it is in place when AGENTS.md is refused. stderr names the project file as written, with its original, and says
// AGENTS.md was not replaced, with its backup. Both ways the second write can fail are run: the file changes after it is
// read, and the rename fails.
func TestPiFailureOnTheSecondFileNamesTheFirst(t *testing.T) {
	t.Run("the AGENTS.md changes after it is read", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, piConfigFile(dir), piExisting)
		writeSettings(t, piAgentsFile(dir), piNotes)
		other := "# Other\n"
		calls := 0
		onBeforeRename(t, func() {
			calls++
			if calls == 2 {
				if err := os.WriteFile(piAgentsFile(dir), []byte(other), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})

		code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
		}
		for _, want := range []string{
			piAgentsFile(dir) + " it changed after enable read it: this file was not replaced, and the original is kept at " + piBackupOf(t, "AGENTS.md"),
			"already written, and not undone: " + piConfigFile(dir) + "; its original is at " + piBackupOf(t, "mcp.json"),
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, stderr)
			}
		}
		if strings.Contains(stderr, "nothing was replaced") {
			t.Errorf("stderr says nothing was replaced, which is false here:\n%s", stderr)
		}
		if string(readBytes(t, piAgentsFile(dir))) != other {
			t.Errorf("AGENTS.md was replaced:\n%s", readBytes(t, piAgentsFile(dir)))
		}
	})
	t.Run("the rename fails", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, piConfigFile(dir), piExisting)
		writeSettings(t, piAgentsFile(dir), piNotes)
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

		code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
		}
		for _, want := range []string{
			"write " + piAgentsFile(dir) + ": injected rename failure; the file was not replaced",
			"already written, and not undone: " + piConfigFile(dir) + "; its original is at " + piBackupOf(t, "mcp.json"),
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, stderr)
			}
		}
		if string(readBytes(t, piAgentsFile(dir))) != piNotes {
			t.Errorf("AGENTS.md changed:\n%s", readBytes(t, piAgentsFile(dir)))
		}
	})
}

// TestPiBlockNamesBothPrefixes checks the block names pi's tool prefix, and says the prefix OpenCode gives the same tools,
// since OpenCode reads AGENTS.md too.
func TestPiBlockNamesBothPrefixes(t *testing.T) {
	block := piBlock()
	for _, want := range []string{"mcp__cairn__task_current", "In OpenCode they start with cairn_", "put cairn_ where the steps say mcp__cairn__"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block lacks %q:\n%s", want, block)
		}
	}
}

// TestPiSaysTheProjectMustBeTrusted checks that enable's own output for pi says pi reads the project file only after the
// project is trusted, on the run that writes the file.
func TestPiSaysTheProjectMustBeTrusted(t *testing.T) {
	enableProject(t)
	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if want := reportLine("trust", "needs trust", "pi reads "+piConfig+" only after the project is trusted"); !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
}

// TestPiUpdatesAnOlderCairnBlock checks a whole block that cairn wrote with older text: the block between its markers is
// replaced by the current text, the text on either side is kept byte for byte, and the original is copied to the backup first.
func TestPiUpdatesAnOlderCairnBlock(t *testing.T) {
	dir := enableProject(t)
	older := piBegin + " (written by cairn enable --agent pi, EXPERIMENTAL; do not edit inside) -->\nan older task loop\n" + piEnd + "\n"
	original := piNotes + "\n" + older + "More notes after the block.\n"
	writeSettings(t, piAgentsFile(dir), original)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := piNotes + "\n" + strings.TrimSuffix(piBlock(), "\n") + "\nMore notes after the block.\n"
	if got := string(readBytes(t, piAgentsFile(dir))); got != want {
		t.Errorf("AGENTS.md is not the notes, the current block and the notes after it:\n%s", got)
	}
	backup := piBackupOf(t, "AGENTS.md")
	if !bytes.Equal(readBytes(t, backup), []byte(original)) {
		t.Errorf("the backup is not the original file:\n%s", readBytes(t, backup))
	}
	if !strings.Contains(stdout, reportLine("instructions", "updated", piAgents+" block")) || !strings.Contains(stdout, "backup "+backup+"\n") {
		t.Errorf("stdout does not report the update and its backup:\n%s", stdout)
	}
	assertExperimental(t, "pi", stdout)
}

// TestPiReplacesMarkersInsideAFencedBlock pins what happens when a user's code fence holds cairn's exact start line and end
// marker, each on a line of its own. The recogniser reads lines, not fences, so those lines are cairn's block: the text between
// them is replaced by the current block, with the original copied to the backup first, and the fence lines and the text around
// them are kept byte for byte.
func TestPiReplacesMarkersInsideAFencedBlock(t *testing.T) {
	dir := enableProject(t)
	original := "# Notes\n\n```markdown\n" + piBeginLine + "\nmy own edits inside the fence\n" + piEnd + "\n```\n\nKeep the tests green.\n"
	writeSettings(t, piAgentsFile(dir), original)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := "# Notes\n\n```markdown\n" + strings.TrimSuffix(piBlock(), "\n") + "\n```\n\nKeep the tests green.\n"
	if got := string(readBytes(t, piAgentsFile(dir))); got != want {
		t.Errorf("AGENTS.md is not the fence around the current block:\n%q", got)
	}
	if backup := piBackupOf(t, "AGENTS.md"); !bytes.Equal(readBytes(t, backup), []byte(original)) {
		t.Errorf("the backup is not the original file:\n%q", readBytes(t, backup))
	}
	if !strings.Contains(stdout, reportLine("instructions", "updated", piAgents+" block")) {
		t.Errorf("stdout does not report the update:\n%s", stdout)
	}
}

// markerInProse is AGENTS.md text that mentions both markers, once each and in order, in the middle of a paragraph. Neither
// mention is a whole line, so the file is not cairn's block.
const markerInProse = "# Notes\n\nThe cairn block runs from <!-- cairn:begin --> down to <!-- cairn:end -->, and enable keeps it there.\n\nKeep the tests green.\n"

// TestPiRefusesMarkersThatAreNotWholeLines checks the AGENTS.md files that hold marker text but no whole block: a mention of
// both markers in prose, a start marker mid-line before a real end line, and a start line that is close to cairn's but not
// exact. Each is refused with exit 1, nothing written, the bytes of AGENTS.md unchanged, and no backup made.
func TestPiRefusesMarkersThatAreNotWholeLines(t *testing.T) {
	cases := []struct {
		name, text string
	}{
		{"a mention of both markers in prose", markerInProse},
		{"a start marker mid-line, then a real end line", "# Notes\n\nSee the old <!-- cairn:begin --> line.\n" + piEnd + "\n"},
		{"a start line close to cairn's text", "# Notes\n\n" + strings.Replace(piBeginLine, "do not edit inside", "edit freely", 1) + "\nmy notes\n" + piEnd + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			writeSettings(t, piAgentsFile(dir), tc.text)

			code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
			if code != 1 || stdout != "" {
				t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
			}
			if !strings.Contains(stderr, piAgentsFile(dir)+" has a cairn block that is not whole") || !strings.Contains(stderr, "fix the markers by hand") {
				t.Errorf("stderr %q does not refuse the file and say to fix the markers by hand", stderr)
			}
			if got := readBytes(t, piAgentsFile(dir)); !bytes.Equal(got, []byte(tc.text)) {
				t.Errorf("AGENTS.md changed; it must stay byte for byte:\n%q", got)
			}
			if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml", piAgents}) {
				t.Errorf("the project holds %v, want the project file and AGENTS.md alone", got)
			}
			if got := listDir(t, filepath.Dir(piBackupOf(t, "AGENTS.md"))); len(got) != 0 {
				t.Errorf("a backup was made of a file that was refused: %v", got)
			}
		})
	}
}

// TestAgentsBlockStates checks how the block's markers are read: none, one whole block, or marker text that is not a block.
func TestAgentsBlockStates(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"an empty file", "", "absent"},
		{"text with no marker", piNotes, "absent"},
		{"one whole block", piBlock(), "whole"},
		{"a whole block among text", piNotes + "\n" + piBlock() + "after\n", "whole"},
		{"a start marker alone", piBegin + " -->\n", "broken"},
		{"an end marker alone", piEnd + "\n", "broken"},
		{"two blocks", piBlock() + piBlock(), "broken"},
		{"an end before a start", piEnd + "\n" + piBegin + " -->\n", "broken"},
		{"both markers mentioned in prose", markerInProse, "broken"},
		{"a start marker mid-line", "x " + piBeginLine + "\n" + piEnd + "\n", "broken"},
		{"a start line with an edited comment", strings.Replace(piBlock(), "do not edit inside", "edit freely", 1), "broken"},
		{"an end marker with text after it", piBeginLine + "\n" + piEnd + " trailing\n", "broken"},
		{"an end marker before its start line", piEnd + "\n" + piBeginLine + "\n", "broken"},
	}
	for _, tc := range cases {
		if got := agentsBlock(tc.text); got != tc.want {
			t.Errorf("%s: agentsBlock is %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestEnableHelpSaysWhatTheExperimentalClientsWrite checks the help names the two experimental clients as experimental,
// says what each writes, and says that pi reads its project file only after the project is trusted.
func TestEnableHelpSaysWhatTheExperimentalClientsWrite(t *testing.T) {
	_, stdout, _ := runCmd(t, "enable", "--help")
	for _, want := range []string{
		"opencode and pi are EXPERIMENTAL",
		"adds the cairn MCP server to opencode.json",
		"adds the cairn MCP server to .pi/mcp.json with exposure direct",
		"pi reads .pi/mcp.json only after the project is trusted",
		"The instruction path, .cairn/instructions.md, is taken relative to the project root",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}

// TestEnableTagsOnlyTheExperimentalClients checks that the tag is for the experimental clients alone: a usage error for an
// unknown agent names the agent, and says nothing about an experimental client.
func TestEnableTagsOnlyTheExperimentalClients(t *testing.T) {
	enableProject(t)
	_, _, stderr := runCmd(t, "enable", "--agent", "nope")
	if strings.Contains(stderr, "EXPERIMENTAL") || !strings.Contains(stderr, "unknown agent") {
		t.Errorf("stderr %q: an unknown agent must not be tagged EXPERIMENTAL", stderr)
	}
}
