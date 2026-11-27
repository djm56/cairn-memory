package main

import (
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
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn"}, "cairnbook": {"command": "cairnbook", "args": ["mcp"], "exposure": "direct"}}}`)
	if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	servers, _ := decodeJSON(t, readBytes(t, piConfigFile(dir)))["mcpServers"].(map[string]any)
	if _, old := servers["cairn"]; old {
		t.Errorf("mcpServers is %v, want no cairn", servers)
	}
}
