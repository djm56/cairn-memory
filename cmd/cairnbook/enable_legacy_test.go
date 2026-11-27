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
