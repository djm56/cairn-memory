package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// enableClock is the UTC time every backup in these tests is named for.
var enableClock = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// backupName is the name of the backup that a run at enableClock makes of a settings file. It lives in the cache
// directory, not the project (backupIn).
const backupName = "settings.local.json.20261105T120000Z"

// backupIn is the backup a run at enableClock makes of the settings file of the project called name. It is under the
// user's cache directory, which useDir points at a fresh directory.
func backupIn(t *testing.T, name string) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "cairn", "backups", name, backupName)
}

// existingSettings is a settings file with keys and hooks that enable does not add to, and a cairn hook with a
// different command, which enable must keep. The number and the characters an encoder would escape by default are
// there, so a rewrite that changed a value would show.
const existingSettings = `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "env": {"NOTE": "a < b && c > d"},
  "count": 12345678901234567890,
  "hooks": {
    "PostToolUse": [{"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "/usr/bin/lint.sh"}]}],
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]},
      {"hooks": [{"type": "command", "command": "cairn hook stop --verbose"}]}
    ]
  }
}
`

// allAdded and allPresent are the outcomes of a run that added every hook, and of one that found them all.
var (
	allAdded   = map[string]string{"SessionStart": "added", "PreCompact": "added", "Stop": "added"}
	allPresent = map[string]string{"SessionStart": "already present", "PreCompact": "already present", "Stop": "already present"}
)

// enableProject makes a project with a project file, runs the commands from it, and fixes the backup clock. It
// returns the project root. Every path a test writes is under it, and useDir points HOME at a fresh directory, so
// no settings file of the machine is reachable. cairnOnPath makes the command findable, so a success prints nothing
// on stderr.
func enableProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\n")
	useDir(t, dir)
	setEnableClock(t)
	cairnOnPath(t)
	return dir
}

// cairnOnPath makes a file named cairn in a fresh directory, and makes that directory the whole PATH for the test, so
// enable finds the command.
func cairnOnPath(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "cairn"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

// setEnableClock fixes the clock a backup's name is read from, until the test ends.
func setEnableClock(t *testing.T) {
	t.Helper()
	prev := enableNow
	enableNow = func() time.Time { return enableClock }
	t.Cleanup(func() { enableNow = prev })
}

// settingsIn is the settings file enable writes for the project at root.
func settingsIn(root string) string {
	return filepath.Join(root, ".claude", settingsName)
}

// writeSettings writes content as the settings file at path, making its directory first.
func writeSettings(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Dir(path), filepath.Base(path), content)
}

// listDir returns the names in dir, sorted. A directory that does not exist has none.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// entryJSON is the entry enable writes for one command, as decodeJSON reads it back.
func entryJSON(matcher, cmdline string) map[string]any {
	entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmdline}}}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return entry
}

// freshHooks is the hooks object enable writes into a file that had none.
func freshHooks() map[string]any {
	return map[string]any{
		"SessionStart": []any{entryJSON("startup|resume|clear|compact", "cairn hook session-start")},
		"PreCompact":   []any{entryJSON("", "cairn hook pre-compact")},
		"Stop":         []any{entryJSON("", "cairn hook stop")},
	}
}

// decodeJSON decodes a JSON object with its numbers kept as their text, so a value is compared as it is written.
func decodeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return v
}

// outcomes reads the hook lines of enable's output: for each event, what happened to its hook.
func outcomes(stdout string) map[string]string {
	out := map[string]string{}
	for _, h := range claudeHooks {
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(line, h.event+" ") && strings.HasSuffix(line, h.cmdline) {
				out[h.event] = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, h.event), h.cmdline))
			}
		}
	}
	return out
}

// TestEnableWritesTheHooksIntoAFreshProject checks that a project with no settings file gets the three hooks in
// the documented shape. The file is created, with no backup, since there was nothing to copy.
func TestEnableWritesTheHooksIntoAFreshProject(t *testing.T) {
	dir := enableProject(t)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	path := settingsIn(dir)
	if got := decodeJSON(t, readBytes(t, path)); !reflect.DeepEqual(got, map[string]any{"hooks": freshHooks()}) {
		t.Errorf("settings file holds %v, want only the three hooks", got)
	}
	if got := outcomes(stdout); !reflect.DeepEqual(got, allAdded) {
		t.Errorf("outcomes %v, want every hook added", got)
	}
	if !strings.Contains(stdout, "wrote "+path+"\n") {
		t.Errorf("stdout does not name the file written:\n%s", stdout)
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file and nothing else", got)
	}
}

// TestEnableGivesANewFileTheUmaskAppliedMode checks that a file enable creates has the mode 0644 before the umask, as
// cairn init makes its file. The expected mode is read from a probe made at 0644 beside the test's directory, so the
// check holds under whatever umask the test runs with.
func TestEnableGivesANewFileTheUmaskAppliedMode(t *testing.T) {
	dir := enableProject(t)
	probe := filepath.Join(t.TempDir(), "probe")
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := runCmd(t, "enable", "--agent", "claude-code"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	info, err := os.Stat(settingsIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want.Mode().Perm() {
		t.Errorf("new file has mode %o, want %o (0644 before the umask)", info.Mode().Perm(), want.Mode().Perm())
	}
}

// TestEnableKeepsEveryOtherKeyAndEntry checks a file with keys and hooks that enable does not add to: each is kept
// with its value, the cairn hook with another command stays, and the original is copied to a backup first.
func TestEnableKeepsEveryOtherKeyAndEntry(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	before := decodeJSON(t, []byte(existingSettings))
	beforeHooks := before["hooks"].(map[string]any)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	after := decodeJSON(t, readBytes(t, path))
	for _, key := range []string{"permissions", "env", "count"} {
		if !reflect.DeepEqual(after[key], before[key]) {
			t.Errorf("%s is %v, was %v", key, after[key], before[key])
		}
	}
	if !strings.Contains(string(readBytes(t, path)), `"a < b && c > d"`) {
		t.Errorf("a character of a kept value was escaped:\n%s", readBytes(t, path))
	}
	hooks := after["hooks"].(map[string]any)
	if !reflect.DeepEqual(hooks["PostToolUse"], beforeHooks["PostToolUse"]) {
		t.Errorf("PostToolUse is %v, was %v", hooks["PostToolUse"], beforeHooks["PostToolUse"])
	}
	wantStop := append(slices.Clone(beforeHooks["Stop"].([]any)), entryJSON("", "cairn hook stop"))
	if !reflect.DeepEqual(hooks["Stop"], wantStop) {
		t.Errorf("Stop is %v, want the two entries it had and then the cairn hook", hooks["Stop"])
	}
	fresh := freshHooks()
	if !reflect.DeepEqual(hooks["SessionStart"], fresh["SessionStart"]) || !reflect.DeepEqual(hooks["PreCompact"], fresh["PreCompact"]) {
		t.Errorf("SessionStart and PreCompact are %v and %v, want the cairn hooks", hooks["SessionStart"], hooks["PreCompact"])
	}
	if got := outcomes(stdout); !reflect.DeepEqual(got, allAdded) {
		t.Errorf("outcomes %v, want every hook added (the existing Stop hook is another command)", got)
	}
	backup := backupIn(t, "acme-api")
	if !bytes.Equal(readBytes(t, backup), []byte(existingSettings)) {
		t.Errorf("the backup is not the original file:\n%s", readBytes(t, backup))
	}
	if !strings.Contains(stdout, "backup "+backup+"\n") {
		t.Errorf("stdout does not name the backup:\n%s", stdout)
	}
}

// TestEnableLeavesAnIdenticalEntryAlone checks a file that already registers the three hooks. It is not written:
// the bytes stay as they were, nothing is backed up, and each hook is reported present. The file is compact on
// purpose, so a rewrite would change its bytes.
func TestEnableLeavesAnIdenticalEntryAlone(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	existing := `{"hooks":{"SessionStart":[{"matcher":"startup|resume|clear|compact","hooks":[{"type":"command","command":"cairn hook session-start"}]}],"PreCompact":[{"hooks":[{"type":"command","command":"cairn hook pre-compact"}]}],"Stop":[{"hooks":[{"type":"command","command":"cairn hook stop"}]}]}}`
	writeSettings(t, path, existing)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if string(readBytes(t, path)) != existing {
		t.Errorf("the file changed:\n%s", readBytes(t, path))
	}
	if got := outcomes(stdout); !reflect.DeepEqual(got, allPresent) {
		t.Errorf("outcomes %v, want every hook already present", got)
	}
	if !strings.Contains(stdout, "unchanged: "+path) {
		t.Errorf("stdout does not say the file is unchanged:\n%s", stdout)
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want no backup", got)
	}
}

// TestEnableMatchesTheCommandNotTheMatcher checks the rule for an existing entry: a cairn command under its event
// counts as present whatever its matcher, and the entry is left as it is. A SessionStart entry without the compact
// matcher is therefore not widened here, and the line names its matcher so the operator can see it.
func TestEnableMatchesTheCommandNotTheMatcher(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"cairn hook session-start"}]}]}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := outcomes(stdout)["SessionStart"]; got != "already present, matcher is startup, not startup|resume|clear|compact" {
		t.Errorf("SessionStart is %q, want the matcher named", got)
	}
	got := decodeJSON(t, readBytes(t, path))["hooks"].(map[string]any)["SessionStart"]
	want := []any{entryJSON("startup", "cairn hook session-start")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionStart is %v, want the entry left as it was", got)
	}
}

// TestEnableReportsAMatcherThatDiffers checks the line for an existing SessionStart entry. An entry with the matcher
// enable writes gets the bare "already present". An entry with no matcher gets the matcher named as (none). Either way
// the entry is kept as it was.
func TestEnableReportsAMatcherThatDiffers(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		want  string
	}{
		{"the same matcher", `{"matcher":"startup|resume|clear|compact","hooks":[{"type":"command","command":"cairn hook session-start"}]}`, "already present"},
		{"no matcher", `{"hooks":[{"type":"command","command":"cairn hook session-start"}]}`, "already present, matcher is (none), not startup|resume|clear|compact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			writeSettings(t, settingsIn(dir), `{"hooks":{"SessionStart":[`+tc.entry+`]}}`)

			code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
			}
			if got := outcomes(stdout)["SessionStart"]; got != tc.want {
				t.Errorf("SessionStart is %q, want %q", got, tc.want)
			}
			got := decodeJSON(t, readBytes(t, settingsIn(dir)))["hooks"].(map[string]any)["SessionStart"]
			if want := []any{decodeJSON(t, []byte(tc.entry))}; !reflect.DeepEqual(got, want) {
				t.Errorf("SessionStart is %v, want the entry left as it was: %v", got, want)
			}
		})
	}
}

// TestEnableRunTwiceGivesTheSameFile checks idempotence from both starting points. A second run writes nothing:
// the file keeps the bytes the first run left, and no second backup is made.
func TestEnableRunTwiceGivesTheSameFile(t *testing.T) {
	cases := []struct {
		name      string
		existing  string
		wantFiles []string
	}{
		{"a fresh project", "", []string{settingsName}},
		{"an existing file", existingSettings, []string{settingsName}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			path := settingsIn(dir)
			if tc.existing != "" {
				writeSettings(t, path, tc.existing)
			}
			if code, _, stderr := runCmd(t, "enable", "--agent", "claude-code"); code != 0 {
				t.Fatalf("first run: exit %d, stderr %q", code, stderr)
			}
			first := readBytes(t, path)

			code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
			if code != 0 || stderr != "" {
				t.Fatalf("second run: exit %d, stderr %q", code, stderr)
			}
			if !bytes.Equal(readBytes(t, path), first) {
				t.Errorf("the second run changed the file:\n%s", readBytes(t, path))
			}
			if got := outcomes(stdout); !reflect.DeepEqual(got, allPresent) {
				t.Errorf("second run outcomes %v, want every hook already present", got)
			}
			if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, tc.wantFiles) {
				t.Errorf(".claude holds %v, want %v", got, tc.wantFiles)
			}
			if tc.existing != "" {
				if got := listDir(t, filepath.Dir(backupIn(t, "acme-api"))); !slices.Equal(got, []string{backupName}) {
					t.Errorf("the backup directory holds %v, want only the backup of the first run", got)
				}
			}
		})
	}
}

// TestEnableRefusesAFileItCannotMerge checks that a file which is not valid JSON, or not the shape enable merges
// into, is refused with exit 1. The file is left as it was, and nothing else is written beside it.
func TestEnableRefusesAFileItCannotMerge(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"not valid JSON", `{"hooks": `},
		{"an empty file", ``},
		{"a top level that is an array", `[]`},
		{"hooks that is an array", `{"hooks": []}`},
		{"hooks that is null", `{"hooks": null}`},
		{"an event that is an object", `{"hooks": {"Stop": {"hooks": []}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			path := settingsIn(dir)
			writeSettings(t, path, tc.content)

			code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
			if code != 1 || stdout != "" {
				t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
			}
			if !strings.Contains(stderr, path) || !strings.Contains(stderr, "Nothing was changed") {
				t.Errorf("stderr %q does not name the file and say nothing was changed", stderr)
			}
			if string(readBytes(t, path)) != tc.content {
				t.Errorf("the file changed:\n%s", readBytes(t, path))
			}
			if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
				t.Errorf(".claude holds %v, want the file and nothing else", got)
			}
		})
	}
}

// TestEnableBacksUpTheOriginalWithItsMode checks that the backup holds the original bytes and the original mode, and
// that the new file keeps the mode too.
func TestEnableBacksUpTheOriginalWithItsMode(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := runCmd(t, "enable", "--agent", "claude-code"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	backup := backupIn(t, "acme-api")
	if !bytes.Equal(readBytes(t, backup), []byte(existingSettings)) {
		t.Errorf("the backup is not the original:\n%s", readBytes(t, backup))
	}
	for _, p := range []string{path, backup} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %o, want 600", p, info.Mode().Perm())
		}
	}
}

// TestEnableBacksUpAWorldReadableFileAs0600 checks that a backup is 0600 even when the file it copies is 0644: the
// backup may hold secrets. The settings file keeps its own 0644.
func TestEnableBacksUpAWorldReadableFileAs0600(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := runCmd(t, "enable", "--agent", "claude-code"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for p, want := range map[string]fs.FileMode{path: 0o644, backupIn(t, "acme-api"): 0o600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %o, want %o", p, info.Mode().Perm(), want)
		}
	}
}

// TestEnableBacksUpOutsideTheProject checks that the backup of an existing settings file is not under the project,
// so no command that reads the project's tree can find a copy of a local settings file there. The backup is in the
// cache directory, and the project's .claude holds the settings file alone.
func TestEnableBacksUpOutsideTheProject(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, settingsIn(dir), existingSettings)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	backup := backupIn(t, "acme-api")
	if rel, err := filepath.Rel(dir, backup); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Errorf("the backup %s is inside the project %s", backup, dir)
	}
	if !strings.Contains(stdout, "backup "+backup+"\n") {
		t.Errorf("stdout does not print the backup's path:\n%s", stdout)
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file and nothing else", got)
	}
}

// TestEnableRefusesWhenNoCacheDirectoryIsKnown checks that an existing file is not written when the backup has no
// place to go: with no home directory and no cache directory set, enable exits 1 and changes nothing.
func TestEnableRefusesWhenNoCacheDirectoryIsKnown(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "nothing was changed") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and nothing changed", code, stdout, stderr)
	}
	if string(readBytes(t, path)) != existingSettings {
		t.Errorf("the file changed:\n%s", readBytes(t, path))
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file and nothing else", got)
	}
}

// TestEnableNeedsNoCacheDirectoryForANewFile checks that a file enable creates has nothing to back up, so the cache
// directory is not needed: a project with no settings file is written even when no cache directory is known.
func TestEnableNeedsNoCacheDirectoryForANewFile(t *testing.T) {
	dir := enableProject(t)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	code, _, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := decodeJSON(t, readBytes(t, settingsIn(dir))); !reflect.DeepEqual(got, map[string]any{"hooks": freshHooks()}) {
		t.Errorf("settings file holds %v, want only the three hooks", got)
	}
}

// TestEnableDryRunWritesNothing checks --dry-run in both starting points: it prints what would be added, and
// creates no directory, file or backup.
func TestEnableDryRunWritesNothing(t *testing.T) {
	t.Run("a fresh project", func(t *testing.T) {
		dir := enableProject(t)

		code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code", "--dry-run")
		if code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
		}
		if got := outcomes(stdout); !reflect.DeepEqual(got, allAdded) {
			t.Errorf("outcomes %v, want every hook added", got)
		}
		if !strings.Contains(stdout, "dry run: nothing written to "+settingsIn(dir)) {
			t.Errorf("stdout does not say nothing was written:\n%s", stdout)
		}
		if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
			t.Errorf("the dry run created .claude (stat error %v)", err)
		}
	})
	t.Run("an existing file", func(t *testing.T) {
		dir := enableProject(t)
		path := settingsIn(dir)
		writeSettings(t, path, existingSettings)

		code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code", "--dry-run")
		if code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
		}
		if got := outcomes(stdout); !reflect.DeepEqual(got, allAdded) {
			t.Errorf("outcomes %v, want every hook added", got)
		}
		if string(readBytes(t, path)) != existingSettings {
			t.Errorf("the dry run changed the file:\n%s", readBytes(t, path))
		}
		if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
			t.Errorf(".claude holds %v, want no backup", got)
		}
	})
}

// TestEnableWarnsWhenCairnIsNotOnThePath checks that a write with cairn off the PATH still exits 0 and writes the file,
// and that the warning goes to stderr and names the PATH.
func TestEnableWarnsWhenCairnIsNotOnThePath(t *testing.T) {
	dir := enableProject(t)
	t.Setenv("PATH", t.TempDir())

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q: want 0, since the warning does not change the exit code", code, stderr)
	}
	if !strings.Contains(stderr, "not on the PATH") {
		t.Errorf("stderr %q does not warn about the PATH", stderr)
	}
	if !strings.Contains(stdout, "wrote "+settingsIn(dir)+"\n") {
		t.Errorf("stdout does not say the file was written:\n%s", stdout)
	}
	if _, err := os.Stat(settingsIn(dir)); err != nil {
		t.Errorf("the settings file was not written: %v", err)
	}
}

// TestEnableWithNoProjectFileExitsOne checks that a directory with no project file is not a project: enable exits 1,
// names the fix, and creates nothing.
func TestEnableWithNoProjectFileExitsOne(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || stdout != "" || stderr == "" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and a message", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Errorf("enable created .claude without a project (stat error %v)", err)
	}
}

// TestEnableWithOnlyCairnProjectSetExitsOne checks that CAIRN_PROJECT alone is refused: the project directory is
// found from the file, and with no file there is nowhere to write.
func TestEnableWithOnlyCairnProjectSetExitsOne(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "unset CAIRN_PROJECT") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1 and the fix named", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Errorf("enable created .claude without a project file (stat error %v)", err)
	}
}

// TestEnableUsageErrors checks the usage errors: no agent, an agent that is not built, an unknown agent, a
// positional argument and an unknown flag all exit 2, print nothing on stdout and write nothing.
func TestEnableUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no agent", []string{"enable"}, "needs --agent"},
		{"opencode is not built yet", []string{"enable", "--agent", "opencode"}, "not built yet"},
		{"pi is not built yet", []string{"enable", "--agent", "pi"}, "not built yet"},
		{"an unknown agent", []string{"enable", "--agent", "nope"}, "unknown agent"},
		{"an argument after the flags", []string{"enable", "--agent", "claude-code", "extra"}, "takes flags only"},
		{"an unknown flag", []string{"enable", "--agent", "claude-code", "--nope"}, "run cairn enable --help"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)

			code, stdout, stderr := runCmd(t, tc.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stdout %q, stderr %q: want 2, nothing on stdout and %q on stderr", code, stdout, stderr, tc.want)
			}
			if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
				t.Errorf("a usage error created .claude (stat error %v)", err)
			}
		})
	}
}

// TestEnableHelpAndRegistry checks that enable is in the dispatch table and the help, and that --help prints its
// usage on stdout and exits 0.
func TestEnableHelpAndRegistry(t *testing.T) {
	code, stdout, stderr := runCmd(t, "enable", "--help")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Usage: cairn enable --agent <name> [--dry-run]") {
		t.Errorf("enable --help: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, ok := commands["enable"]; !ok {
		t.Errorf("enable is not in the dispatch table")
	}
	_, help, _ := runCmd(t, "help")
	if !strings.Contains(help, "  enable ") {
		t.Errorf("help does not list enable:\n%s", help)
	}
}

// TestEnableWritesToTheProjectAboveTheWorkingDirectory checks that enable finds the project file as init and doctor
// do, walking up from the working directory, and writes the settings file beside that file.
func TestEnableWritesToTheProjectAboveTheWorkingDirectory(t *testing.T) {
	root := enableProject(t)
	sub := filepath.Join(root, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	useDir(t, sub)

	if code, _, stderr := runCmd(t, "enable", "--agent", "claude-code"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(settingsIn(root)); err != nil {
		t.Errorf("the settings file is not beside the project file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".claude")); !os.IsNotExist(err) {
		t.Errorf("enable wrote into the working directory (stat error %v)", err)
	}
}

// TestEnableRefusesASymlinkAndTouchesItsTarget checks that a settings file which is a symbolic link is refused: a
// rename would replace the link, so the file it names is left alone.
func TestEnableRefusesASymlinkAndTouchesItsTarget(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeFile(t, dir, "elsewhere.json", existingSettings)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere.json"), path); err != nil {
		t.Skipf("this system cannot make a symbolic link: %v", err)
	}

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "symbolic link") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1 and the link named", code, stdout, stderr)
	}
	if string(readBytes(t, filepath.Join(dir, "elsewhere.json"))) != existingSettings {
		t.Errorf("the file the link names was changed")
	}
}

// onBeforeRename runs f at the moment before the check and the rename, until the test ends. It stands in for another
// writer that saves the settings file while enable is writing it.
func onBeforeRename(t *testing.T, f func()) {
	t.Helper()
	prev := beforeRename
	beforeRename = f
	t.Cleanup(func() { beforeRename = prev })
}

// TestEnableRefusesAFileThatChangesWhileItIsWritten checks that a settings file another writer saves between enable's
// read and its rename is not replaced. enable exits 1, the other writer's file is kept, the temporary file is removed,
// and the backup holds the original.
func TestEnableRefusesAFileThatChangesWhileItIsWritten(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	other := "{\"permissions\":{\"allow\":[]}}\n"
	onBeforeRename(t, func() {
		if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || !strings.Contains(stderr, "changed after enable read it") || !strings.Contains(stderr, "nothing was replaced") {
		t.Errorf("exit %d, stderr %q: want 1 and the refusal named", code, stderr)
	}
	if strings.Contains(stdout, "wrote ") {
		t.Errorf("stdout says the file was written:\n%s", stdout)
	}
	if string(readBytes(t, path)) != other {
		t.Errorf("the other writer's file was replaced:\n%s", readBytes(t, path))
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file and no temporary file", got)
	}
	if !bytes.Equal(readBytes(t, backupIn(t, "acme-api")), []byte(existingSettings)) {
		t.Errorf("the backup is not the original file")
	}
}

// TestEnableRefusesAFileThatAppearsWhileItIsWritten checks the other case: a project with no settings file, where
// another writer creates one between enable's check and its rename. The rename would replace it, so it is refused.
func TestEnableRefusesAFileThatAppearsWhileItIsWritten(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	other := "{\"permissions\":{\"allow\":[]}}\n"
	onBeforeRename(t, func() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	code, _, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || !strings.Contains(stderr, "changed after enable read it") {
		t.Errorf("exit %d, stderr %q: want 1 and the refusal named", code, stderr)
	}
	if string(readBytes(t, path)) != other {
		t.Errorf("the file that appeared was replaced:\n%s", readBytes(t, path))
	}
}

// TestEnableRefusesABackupNameThatIsTaken checks that a backup is never written over an earlier one. The name a run
// would give its backup is already a file, so enable exits 1 before it changes the settings file, and the earlier
// file is kept.
func TestEnableRefusesABackupNameThatIsTaken(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	backup := backupIn(t, "acme-api")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	const earlier = "an earlier backup\n"
	writeFile(t, filepath.Dir(backup), filepath.Base(backup), earlier)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "nothing was changed") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, nothing on stdout and nothing changed", code, stdout, stderr)
	}
	if string(readBytes(t, path)) != existingSettings {
		t.Errorf("the settings file changed:\n%s", readBytes(t, path))
	}
	if string(readBytes(t, backup)) != earlier {
		t.Errorf("the earlier backup was written over:\n%s", readBytes(t, backup))
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file and nothing else", got)
	}
}

// TestEnableKeepsTheFileWhenTheRenameFails checks a rename that fails: the old file is intact, no temporary file is
// left beside it, and the backup holds the original.
func TestEnableKeepsTheFileWhenTheRenameFails(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, existingSettings)
	prev := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameFile = prev })

	code, _, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 1 || !strings.Contains(stderr, "the file was not replaced") {
		t.Errorf("exit %d, stderr %q: want 1 and the file named as not replaced", code, stderr)
	}
	if string(readBytes(t, path)) != existingSettings {
		t.Errorf("the old file is not intact:\n%s", readBytes(t, path))
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !slices.Equal(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file and no temporary file", got)
	}
	if !bytes.Equal(readBytes(t, backupIn(t, "acme-api")), []byte(existingSettings)) {
		t.Errorf("the backup is not the original file")
	}
}
