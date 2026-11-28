package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEnableRenamesTheOldHookCommands checks a settings file written by enable before the rename to Cairnbook: its hooks
// run the old binary name, cairn, so enable changes each one to cairnbook in place, keeps every other entry, and adds no
// second entry beside it.
func TestEnableRenamesTheOldHookCommands(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, settingsIn(dir), `{
  "hooks": {
    "SessionStart": [{"matcher": "startup|resume|clear|compact", "hooks": [{"type": "command", "command": "cairn hook session-start"}]}],
    "PreCompact": [{"hooks": [{"type": "command", "command": "cairn hook pre-compact"}]}],
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]},
      {"hooks": [{"type": "command", "command": "cairn hook stop"}]}
    ]
  }
}
`)
	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	hooks := freshHooks()
	hooks["Stop"] = []any{entryJSON("", "notify-send done"), entryJSON("", "cairnbook hook stop")}
	if got := decodeJSON(t, readBytes(t, settingsIn(dir))); !reflect.DeepEqual(got, map[string]any{"hooks": hooks}) {
		t.Errorf("settings file holds %v, want the old commands renamed in place", got)
	}
	want := map[string]string{
		"SessionStart": "renamed from cairn hook session-start",
		"PreCompact":   "renamed from cairn hook pre-compact",
		"Stop":         "renamed from cairn hook stop",
	}
	if got := outcomes(stdout); !reflect.DeepEqual(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	// A second run finds them all.
	if _, stdout, _ = runCmd(t, "enable", "--agent", "claude-code"); !reflect.DeepEqual(outcomes(stdout), allPresent) {
		t.Errorf("second run outcomes %v, want every hook already present", outcomes(stdout))
	}
}

// TestEnableWarnsOfThePluginUnderItsOldName checks that a plugin still enabled as cairn, its name before the rename, is
// named in a warning that says to reinstall it, and that enable still adds the hooks, since the old plugin's hooks run a
// binary that is gone.
func TestEnableWarnsOfThePluginUnderItsOldName(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, settingsIn(dir), `{"enabledPlugins": {"cairn@cairn-memory": true}}`)
	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q: want 0", code, stderr)
	}
	if !strings.Contains(stderr, "old name cairn") || !strings.Contains(stderr, "cairnbook@cairnbook") {
		t.Errorf("stderr %q does not warn of the old plugin", stderr)
	}
	if got := outcomes(stdout); !reflect.DeepEqual(got, allAdded) {
		t.Errorf("outcomes %v, want every hook added", got)
	}
}

// TestOpenCodeRemovesTheOldEntry checks an OpenCode config written before the rename: the old cairn server and the old
// instruction file's place in the list are removed, and cairnbook is registered in their place.
func TestOpenCodeRemovesTheOldEntry(t *testing.T) {
	dir := enableProject(t)
	writeFile(t, dir, openCodeFile, `{"mcp": {"cairn": {"type": "local", "command": ["cairn", "mcp"]}, "other": {"type": "local"}}, "instructions": [".cairn/instructions.md", "mine.md"]}`)
	code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := map[string]any{
		"mcp":          map[string]any{"cairnbook": ocCairnbookEntry(), "other": map[string]any{"type": "local"}},
		"instructions": []any{"mine.md", openCodeInstructions},
	}
	if got := decodeJSON(t, readBytes(t, ocConfig(dir))); !reflect.DeepEqual(got, want) {
		t.Errorf("opencode.json holds %v, want %v", got, want)
	}
	if !strings.Contains(stdout, "the old cairn entry was removed") {
		t.Errorf("stdout does not say the old entry was removed:\n%s", stdout)
	}
}

// TestPiRemovesTheOldEntry checks a pi config written before the rename: the old cairn server is removed and cairnbook is
// registered.
func TestPiRemovesTheOldEntry(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}}`)
	code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	servers, _ := decodeJSON(t, readBytes(t, piConfigFile(dir)))["mcpServers"].(map[string]any)
	if _, old := servers["cairn"]; old || servers["cairnbook"] == nil {
		t.Errorf("mcpServers is %v, want cairnbook and no cairn", servers)
	}
	if !strings.Contains(stdout, "the old cairn entry was removed") {
		t.Errorf("stdout does not say the old entry was removed:\n%s", stdout)
	}
}

// TestPiRemovesTheOldEntryBesideANewOne checks that an old cairn server is removed even when cairnbook is already
// registered, which is the only change then.
func TestPiRemovesTheOldEntryBesideANewOne(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}, "cairnbook": {"command": "cairnbook", "args": ["mcp"], "exposure": "direct"}}}`)
	if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	servers, _ := decodeJSON(t, readBytes(t, piConfigFile(dir)))["mcpServers"].(map[string]any)
	if _, old := servers["cairn"]; old {
		t.Errorf("mcpServers is %v, want no cairn", servers)
	}
}

// TestEnableRemovesTheOldHookBesideTheNewOne checks a settings file that already holds both the old cairn hook and the
// new cairnbook hook for an event: enable removes the old handler and does not rename it, so the hook is not run twice.
// The three shapes are an old entry beside a new one, an old handler in the same entry as a new one, and a bare old
// entry, which leaves no handler once removed and so goes. Each event is reported as removed, and a second run finds
// every hook already present.
func TestEnableRemovesTheOldHookBesideTheNewOne(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, settingsIn(dir), `{
  "hooks": {
    "SessionStart": [
      {"matcher": "startup|resume|clear|compact", "hooks": [{"type": "command", "command": "cairnbook hook session-start"}]},
      {"matcher": "startup|resume|clear|compact", "hooks": [{"type": "command", "command": "cairn hook session-start"}]}
    ],
    "PreCompact": [
      {"hooks": [{"type": "command", "command": "cairnbook hook pre-compact"}]},
      {"hooks": [{"type": "command", "command": "cairn hook pre-compact"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "cairn hook stop"}, {"type": "command", "command": "cairnbook hook stop"}]}
    ]
  }
}
`)
	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := decodeJSON(t, readBytes(t, settingsIn(dir))); !reflect.DeepEqual(got, map[string]any{"hooks": freshHooks()}) {
		t.Errorf("settings file holds %v, want one new entry per event and no old command", got)
	}
	want := map[string]string{"SessionStart": "old entry removed", "PreCompact": "old entry removed", "Stop": "old entry removed"}
	if got := outcomes(stdout); !reflect.DeepEqual(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	if _, stdout, _ = runCmd(t, "enable", "--agent", "claude-code"); !reflect.DeepEqual(outcomes(stdout), allPresent) {
		t.Errorf("second run outcomes %v, want every hook already present", outcomes(stdout))
	}
}

// legacyCase is one entry under the name cairn, as the config holds it, and whether enable removes it.
type legacyCase struct {
	what    string
	entry   string
	removed bool
}

// jsonValue decodes one JSON value the way decodeJSON decodes a file, so a value read back from a
// config can be compared with the literal it was written from.
func jsonValue(t *testing.T, literal string) any {
	t.Helper()
	return decodeJSON(t, []byte(`{"v": `+literal+`}`))["v"]
}

// TestOpenCodeRemovesCairnOnlyWhenItRunsCairnMCP checks the rule for OpenCode: enable removes an entry
// named cairn only when its command is cairn, or a path whose base name is cairn, followed by exactly
// "mcp". Any other entry under that name is left as it is, and the report says so. Each case has its
// own project, since the fixed clock makes backup names collide within one.
func TestOpenCodeRemovesCairnOnlyWhenItRunsCairnMCP(t *testing.T) {
	cases := []legacyCase{
		{"the old entry by name", `{"type": "local", "command": ["cairn", "mcp"]}`, true},
		{"the old entry by absolute path, with other keys", `{"type": "local", "command": ["/opt/tools/bin/cairn", "mcp"], "timeout": 5000, "env": {"A": "1"}}`, true},
		{"a user server that runs node", `{"type": "local", "command": ["node", "cairn.js"]}`, false},
		{"a server that runs cairn with another argument", `{"type": "local", "command": ["cairn", "serve"]}`, false},
		{"a server that runs cairn with no argument", `{"type": "local", "command": ["cairn"]}`, false},
		{"a server that runs cairn with an extra argument", `{"type": "local", "command": ["cairn", "mcp", "--verbose"]}`, false},
		{"a Windows name with an exe suffix", `{"type": "local", "command": ["cairn.exe", "mcp"]}`, false},
		{"a command given as a string", `{"type": "local", "command": "cairn mcp"}`, false},
		{"a command that is not a list of strings", `{"type": "local", "command": [1, "mcp"]}`, false},
		{"an entry that is not an object", `"cairn mcp"`, false},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			dir := enableProject(t)
			writeFile(t, dir, openCodeFile, `{"mcp": {"cairn": `+c.entry+`}}`)
			code, stdout, stderr := runCmd(t, "enable", "--agent", "opencode")
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
			}
			mcp, _ := decodeJSON(t, readBytes(t, ocConfig(dir)))["mcp"].(map[string]any)
			if _, has := mcp["cairnbook"]; !has {
				t.Errorf("mcp is %v, want cairnbook registered", mcp)
			}
			got, has := mcp["cairn"]
			if has == c.removed {
				t.Errorf("cairn present = %v, want removed = %v: mcp is %v", has, c.removed, mcp)
			}
			if !c.removed && has && !reflect.DeepEqual(got, jsonValue(t, c.entry)) {
				t.Errorf("the entry left as is changed: %v, want %s", got, c.entry)
			}
			if c.removed && !strings.Contains(stdout, "the old cairn entry was removed") {
				t.Errorf("stdout does not say the old entry was removed:\n%s", stdout)
			}
			if !c.removed && !strings.Contains(stdout, "a server named cairn that does not run cairn mcp was left as is") {
				t.Errorf("stdout does not say the cairn entry was left as is:\n%s", stdout)
			}
		})
	}
}

// TestPiRemovesCairnOnlyWhenItRunsCairnMCP checks the rule for pi: enable removes an entry named cairn
// only when its command is cairn, or a path whose base name is cairn, and its args are exactly ["mcp"].
// Any other entry under that name is left as it is, and the report says so. Each case has its own
// project, since the fixed clock makes backup names collide within one.
func TestPiRemovesCairnOnlyWhenItRunsCairnMCP(t *testing.T) {
	cases := []legacyCase{
		{"the old entry by name", `{"command": "cairn", "args": ["mcp"], "exposure": "direct"}`, true},
		{"the old entry by absolute path", `{"command": "/opt/tools/bin/cairn", "args": ["mcp"]}`, true},
		{"a user server named cairn that runs cairn-tools", `{"command": "cairn-tools", "args": ["mcp"]}`, false},
		{"a user server that runs node", `{"command": "node", "args": ["cairn.js"]}`, false},
		{"a server that runs cairn with another argument", `{"command": "cairn", "args": ["serve"]}`, false},
		{"a server that runs cairn with an extra argument", `{"command": "cairn", "args": ["mcp", "--verbose"]}`, false},
		{"a Windows name with an exe suffix", `{"command": "cairn.exe", "args": ["mcp"]}`, false},
		{"a server that runs cairn with no args", `{"command": "cairn"}`, false},
		{"a command given as a list", `{"command": ["cairn", "mcp"]}`, false},
		{"args given as a string", `{"command": "cairn", "args": "mcp"}`, false},
		{"an entry that is not an object", `"cairn mcp"`, false},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			dir := enableProject(t)
			writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": `+c.entry+`}}`)
			code, stdout, stderr := runCmd(t, "enable", "--agent", "pi")
			if code != 0 || stderr != "" {
				t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
			}
			servers, _ := decodeJSON(t, readBytes(t, piConfigFile(dir)))["mcpServers"].(map[string]any)
			if _, has := servers["cairnbook"]; !has {
				t.Errorf("mcpServers is %v, want cairnbook registered", servers)
			}
			got, has := servers["cairn"]
			if has == c.removed {
				t.Errorf("cairn present = %v, want removed = %v: mcpServers is %v", has, c.removed, servers)
			}
			if !c.removed && has && !reflect.DeepEqual(got, jsonValue(t, c.entry)) {
				t.Errorf("the entry left as is changed: %v, want %s", got, c.entry)
			}
			if c.removed && !strings.Contains(stdout, "the old cairn entry was removed") {
				t.Errorf("stdout does not say the old entry was removed:\n%s", stdout)
			}
			if !c.removed && !strings.Contains(stdout, "a server named cairn that does not run cairn mcp was left as is") {
				t.Errorf("stdout does not say the cairn entry was left as is:\n%s", stdout)
			}
		})
	}
}

// TestEnableDryRunReportsARenameAndWritesNothing checks --dry-run over a settings file that holds the old cairn
// hook: the report names the rename, and the file keeps every byte it had and no other file appears in .claude.
func TestEnableDryRunReportsARenameAndWritesNothing(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, `{"hooks": {"SessionStart": [{"matcher": "startup|resume|clear|compact", "hooks": [{"type": "command", "command": "cairn hook session-start"}]}]}}`)
	before := readBytes(t, path)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code", "--dry-run")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := outcomes(stdout)["SessionStart"]; got != "renamed from cairn hook session-start" {
		t.Errorf("SessionStart is %q, want the rename reported", got)
	}
	if !strings.Contains(stdout, "dry run: nothing written to "+path) {
		t.Errorf("stdout does not say nothing was written:\n%s", stdout)
	}
	if string(readBytes(t, path)) != string(before) {
		t.Errorf("the dry run changed the file:\n%s", readBytes(t, path))
	}
	if got := listDir(t, filepath.Join(dir, ".claude")); !reflect.DeepEqual(got, []string{settingsName}) {
		t.Errorf(".claude holds %v, want the settings file only", got)
	}
}

// TestEnableRenamesAnOldHookUnderADifferentMatcher pins what enable does with an old cairn hook under a matcher
// that does not name compact. The hook is renamed in place and keeps its matcher. The first run reports the rename
// and the matcher in one line, joined by "; ", so the operator sees the matcher on the run that changes the file; a
// second run reports the matcher alone, as already present.
func TestEnableRenamesAnOldHookUnderADifferentMatcher(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, `{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "cairn hook session-start"}]}]}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := outcomes(stdout)["SessionStart"]; got != "renamed from cairn hook session-start; matcher is startup, not startup|resume|clear|compact" {
		t.Errorf("SessionStart is %q, want the rename and the matcher reported together", got)
	}
	got := decodeJSON(t, readBytes(t, path))["hooks"].(map[string]any)["SessionStart"]
	if want := []any{entryJSON("startup", "cairnbook hook session-start")}; !reflect.DeepEqual(got, want) {
		t.Errorf("SessionStart is %v, want the renamed entry under its own matcher %v", got, want)
	}
	if _, stdout, _ = runCmd(t, "enable", "--agent", "claude-code"); outcomes(stdout)["SessionStart"] != "already present, matcher is startup, not startup|resume|clear|compact" {
		t.Errorf("second run SessionStart is %q, want the matcher named as not covering compact", outcomes(stdout)["SessionStart"])
	}
}

// TestEnableRenamesAnOldHookUnderACompactMatcherThatDiffers pins the rename under a matcher that covers compact but
// is not the hook's own. The rename line names the matcher, as the already-present line does, without the word "not",
// since the entry still runs after /compact. A rename under the hook's own matcher is plain (see
// TestEnableRenamesTheOldHookCommands).
func TestEnableRenamesAnOldHookUnderACompactMatcherThatDiffers(t *testing.T) {
	dir := enableProject(t)
	path := settingsIn(dir)
	writeSettings(t, path, `{"hooks": {"SessionStart": [{"matcher": "compact", "hooks": [{"type": "command", "command": "cairn hook session-start"}]}]}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := outcomes(stdout)["SessionStart"]; got != "renamed from cairn hook session-start; matcher is compact" {
		t.Errorf("SessionStart is %q, want the rename and the matcher named without \"not\"", got)
	}
	if _, stdout, _ = runCmd(t, "enable", "--agent", "claude-code"); outcomes(stdout)["SessionStart"] != "already present, matcher is compact" {
		t.Errorf("second run SessionStart is %q, want the matcher named as already present", outcomes(stdout)["SessionStart"])
	}
}

// TestEnableRemovesTheOldHookBesideANewOneUnderADifferentMatcher pins the removed state under a matcher that differs.
// The old entry is dropped, and the hook still runs once under the new entry's matcher, which is named on the same
// line, as the rename states do. The new entry is left as it is.
func TestEnableRemovesTheOldHookBesideANewOneUnderADifferentMatcher(t *testing.T) {
	dir := enableProject(t)
	writeSettings(t, settingsIn(dir), `{"hooks": {"SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "cairnbook hook session-start"}]},
      {"matcher": "startup|resume|clear|compact", "hooks": [{"type": "command", "command": "cairn hook session-start"}]}
    ]}}`)

	code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	if got := outcomes(stdout)["SessionStart"]; got != "old entry removed; matcher is startup, not startup|resume|clear|compact" {
		t.Errorf("SessionStart is %q, want the removal and the surviving entry's matcher reported together", got)
	}
	want := []any{entryJSON("startup", "cairnbook hook session-start")}
	if got := decodeJSON(t, readBytes(t, settingsIn(dir)))["hooks"].(map[string]any)["SessionStart"]; !reflect.DeepEqual(got, want) {
		t.Errorf("SessionStart is %v, want the old entry dropped and the new one kept as it is", got)
	}
}
