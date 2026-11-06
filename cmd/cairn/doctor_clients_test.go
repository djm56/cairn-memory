package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// treeOf lists every path under dir, relative to it and sorted, so a test can see that a command left a tree as it was.
func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		names = append(names, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	return names
}

// TestDoctorOpenCodeOnAnEmptyProject checks a project with no OpenCode files: the registration and the instructions are
// warnings that say what to run, and nothing fails.
func TestDoctorOpenCodeOnAnEmptyProject(t *testing.T) {
	dir := agentProject(t)

	checks := experimentalChecks("opencode", dir)
	if c := checkNamed(checks, "mcp"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "not registered") {
		t.Errorf("mcp is %+v, want a warning that cairn is not registered", c)
	}
	if c := checkNamed(checks, "instructions"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "is not listed") {
		t.Errorf("instructions is %+v, want a warning that the file is not listed", c)
	}
	if anyFailed(checks) {
		t.Errorf("a missing registration failed the check: %v", checks)
	}
}

// TestDoctorOpenCodeAfterEnable checks a project that enable has set up: both parts are ok.
func TestDoctorOpenCodeAfterEnable(t *testing.T) {
	dir := agentProject(t)
	if code, _, stderr := runCmd(t, "enable", "--agent", "opencode"); code != 0 {
		t.Fatalf("enable: exit %d, stderr %q", code, stderr)
	}

	checks := experimentalChecks("opencode", dir)
	for _, name := range []string{"mcp", "instructions"} {
		if c := checkNamed(checks, name); c == nil || c.status != "ok" {
			t.Errorf("%s is %+v, want ok after enable", name, c)
		}
	}
}

// TestDoctorOpenCodeWarnsOnAJsoncConfig checks a project with opencode.jsonc: doctor warns that OpenCode reads that file and
// enable refuses to write beside it, and nothing fails.
func TestDoctorOpenCodeWarnsOnAJsoncConfig(t *testing.T) {
	dir := agentProject(t)
	writeSettings(t, filepath.Join(dir, openCodeJSONC), "{}\n")

	checks := experimentalChecks("opencode", dir)
	if c := checkNamed(checks, "config"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "is there, and OpenCode reads it") {
		t.Errorf("config is %+v, want a warning about opencode.jsonc", c)
	}
	if anyFailed(checks) {
		t.Errorf("an opencode.jsonc failed the check: %v", checks)
	}
}

// TestDoctorIgnoresTheRestOfTheCairnEntry checks that doctor compares the fields enable writes and no others. An OpenCode
// entry with another timeout and an extra key is ok, and so is a pi entry with an extra key. A pi exposure other than direct
// is still a warning, since the default would hide the tools from the model.
func TestDoctorIgnoresTheRestOfTheCairnEntry(t *testing.T) {
	t.Run("opencode with another timeout and an extra key", func(t *testing.T) {
		dir := agentProject(t)
		writeSettings(t, ocConfig(dir), `{"mcp": {"cairn": {"type": "local", "command": ["cairn", "mcp"], "timeout": 5000, "environment": {"A": "b"}}}}`)
		if c := checkNamed(experimentalChecks("opencode", dir), "mcp"); c == nil || c.status != "ok" {
			t.Errorf("mcp is %+v, want ok: the timeout and the extra key are the person's", c)
		}
	})
	t.Run("pi with an extra key", func(t *testing.T) {
		dir := agentProject(t)
		writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct", "lifecycle": "lazy"}}}`)
		if c := checkNamed(experimentalChecks("pi", dir), "mcp"); c == nil || c.status != "ok" {
			t.Errorf("mcp is %+v, want ok: the extra key is the person's", c)
		}
	})
	t.Run("pi with another exposure", func(t *testing.T) {
		dir := agentProject(t)
		writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "codemode"}}}`)
		if c := checkNamed(experimentalChecks("pi", dir), "mcp"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "with other settings") {
			t.Errorf("mcp is %+v, want a warning about other settings", c)
		}
	})
}

// TestDoctorOpenCodeReadsCRLFAsTheSameText checks an instruction file with the current text and CRLF line endings, as a
// checkout with autocrlf gives it: doctor reports it as holding the task loop.
func TestDoctorOpenCodeReadsCRLFAsTheSameText(t *testing.T) {
	dir := agentProject(t)
	if code, _, stderr := runCmd(t, "enable", "--agent", "opencode"); code != 0 {
		t.Fatalf("enable: exit %d, stderr %q", code, stderr)
	}
	writeSettings(t, ocInstructions(dir), strings.ReplaceAll(openCodeInstructionsText(), "\n", "\r\n"))

	if c := checkNamed(experimentalChecks("opencode", dir), "instructions"); c == nil || c.status != "ok" {
		t.Errorf("instructions is %+v, want ok for the same text with CRLF line endings", c)
	}
}

// TestDoctorOpenCodeFlagsWhatIsNotRight checks each way the OpenCode files can be off: a cairn server with other settings,
// a listed file that is not there, a file with other text, and a config enable would refuse.
func TestDoctorOpenCodeFlagsWhatIsNotRight(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		setup   func(t *testing.T, dir string)
		check   string
		status  string
		contain string
	}{
		{
			name:   "a cairn server with other settings",
			config: `{"mcp": {"cairn": {"type": "local", "command": ["cairn", "serve"]}}, "instructions": [".cairn/instructions.md"]}`,
			setup: func(t *testing.T, dir string) {
				writeSettings(t, filepath.Join(dir, ".cairn", "instructions.md"), instructionsFor("cairn_"))
			},
			check: "mcp", status: "warn", contain: "with other settings",
		},
		{
			name:   "a listed file that is not there",
			config: `{"instructions": [".cairn/instructions.md"]}`,
			check:  "instructions", status: "warn", contain: "is not there",
		},
		{
			name:   "a listed file with other text",
			config: `{"instructions": [".cairn/instructions.md"]}`,
			setup: func(t *testing.T, dir string) {
				writeSettings(t, filepath.Join(dir, ".cairn", "instructions.md"), "my notes\n")
			},
			check: "instructions", status: "warn", contain: "holds other text",
		},
		{
			name:   "a listed file with an older cairn task loop",
			config: `{"instructions": [".cairn/instructions.md"]}`,
			setup: func(t *testing.T, dir string) {
				writeSettings(t, filepath.Join(dir, ".cairn", "instructions.md"), instructionsMarker+"\nan older task loop\n")
			},
			check: "instructions", status: "warn", contain: "holds an older cairn task loop",
		},
		{
			name:   "a config that is not plain JSON",
			config: `{"mcp": `,
			check:  "config", status: "warn", contain: "is not plain JSON (comments or a syntax error): cairn enable refuses it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := agentProject(t)
			writeSettings(t, ocConfig(dir), tc.config)
			if tc.setup != nil {
				tc.setup(t, dir)
			}

			c := checkNamed(experimentalChecks("opencode", dir), tc.check)
			if c == nil || c.status != tc.status || !strings.Contains(c.detail, tc.contain) {
				t.Errorf("%s is %+v, want %s with %q", tc.check, c, tc.status, tc.contain)
			}
		})
	}
}

// TestDoctorPiOnAnEmptyProject checks a project with no pi files: both parts are warnings that say what to run.
func TestDoctorPiOnAnEmptyProject(t *testing.T) {
	dir := agentProject(t)

	checks := experimentalChecks("pi", dir)
	if c := checkNamed(checks, "mcp"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "is not there") {
		t.Errorf("mcp is %+v, want a warning that .pi/mcp.json is not there", c)
	}
	if c := checkNamed(checks, "instructions"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "no cairn block") {
		t.Errorf("instructions is %+v, want a warning that there is no block", c)
	}
	if anyFailed(checks) {
		t.Errorf("a missing registration failed the check: %v", checks)
	}
}

// TestDoctorPiAfterEnable checks a project that enable has set up for pi: both parts are ok, and the server line says the
// project file is read only after the project is trusted.
func TestDoctorPiAfterEnable(t *testing.T) {
	dir := agentProject(t)
	if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 {
		t.Fatalf("enable: exit %d, stderr %q", code, stderr)
	}

	checks := experimentalChecks("pi", dir)
	if c := checkNamed(checks, "mcp"); c == nil || c.status != "ok" || !strings.Contains(c.detail, "only after the project is trusted") {
		t.Errorf("mcp is %+v, want ok, with the trust note", c)
	}
	if c := checkNamed(checks, "instructions"); c == nil || c.status != "ok" {
		t.Errorf("instructions is %+v, want ok after enable", c)
	}
}

// TestDoctorPiFlagsWhatIsNotRight checks the pi files when they are off: a cairn server with other settings, and an
// AGENTS.md whose block is broken.
func TestDoctorPiFlagsWhatIsNotRight(t *testing.T) {
	dir := agentProject(t)
	writeSettings(t, piConfigFile(dir), `{"mcpServers": {"cairn": {"command": "cairn", "args": ["serve"]}}}`)
	writeSettings(t, piAgentsFile(dir), piBegin+" -->\nno end\n")

	checks := experimentalChecks("pi", dir)
	if c := checkNamed(checks, "mcp"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "with other settings") {
		t.Errorf("mcp is %+v, want a warning about other settings", c)
	}
	if c := checkNamed(checks, "instructions"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "not whole") {
		t.Errorf("instructions is %+v, want a warning that the block is not whole", c)
	}
}

// TestDoctorPiWarnsOnAnOlderBlock checks an AGENTS.md whose cairn block is whole but holds older text: doctor warns and says
// to run enable, and a block that is current is ok.
func TestDoctorPiWarnsOnAnOlderBlock(t *testing.T) {
	dir := agentProject(t)
	older := piBegin + " (written by cairn enable --agent pi, EXPERIMENTAL; do not edit inside) -->\nan older task loop\n" + piEnd + "\n"
	writeSettings(t, piAgentsFile(dir), older)
	if c := checkNamed(experimentalChecks("pi", dir), "instructions"); c == nil || c.status != "warn" || !strings.Contains(c.detail, "is an older text") {
		t.Errorf("instructions is %+v, want a warning about the older text", c)
	}

	if code, _, stderr := runCmd(t, "enable", "--agent", "pi"); code != 0 {
		t.Fatalf("enable: exit %d, stderr %q", code, stderr)
	}
	if c := checkNamed(experimentalChecks("pi", dir), "instructions"); c == nil || c.status != "ok" {
		t.Errorf("instructions is %+v, want ok after enable updated the block", c)
	}
}

// TestDoctorPiAgreesWithEnableOnMarkerText checks that doctor and enable read the same AGENTS.md. Where enable refuses a file,
// doctor warns that the block is not whole, and the refused bytes stay as they were. Where enable updates a block, doctor
// warns about the older text first, and reports the block current once enable has updated it. Where the block is current
// with CRLF line endings, doctor reports it ok, and enable leaves the bytes as they are.
func TestDoctorPiAgreesWithEnableOnMarkerText(t *testing.T) {
	cases := []struct {
		name, text string
		refused    bool   // enable refuses the file
		kept       bool   // enable leaves the bytes as they are
		warn       string // what doctor says of the block before enable runs; "" for ok
	}{
		{"a mention of both markers in prose", markerInProse, true, false, "not whole"},
		{"a start marker mid-line, then a real end line", "# Notes\n\nSee the old " + piBegin + " line.\n" + piEnd + "\n", true, false, "not whole"},
		{"a start line close to cairn's text", strings.Replace(piBlock(), "do not edit inside", "edit freely", 1), true, false, "not whole"},
		{"an older cairn block", piNotes + "\n" + piBeginLine + "\nan older task loop\n" + piEnd + "\nMore notes.\n", false, false, "older text"},
		{"the current block with CRLF line endings", strings.ReplaceAll(piNotes+"\n"+piBlock()+"More notes.\n", "\n", "\r\n"), false, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := enableProject(t)
			writeSettings(t, piAgentsFile(dir), tc.text)

			c := checkNamed(experimentalChecks("pi", dir), "instructions")
			if c == nil || (tc.warn == "" && c.status != "ok") || (tc.warn != "" && (c.status != "warn" || !strings.Contains(c.detail, tc.warn))) {
				t.Errorf("instructions is %+v, want ok or a warning with %q", c, tc.warn)
			}
			code, _, _ := runCmd(t, "enable", "--agent", "pi")
			if tc.refused {
				if code != 1 || string(readBytes(t, piAgentsFile(dir))) != tc.text {
					t.Errorf("enable exit %d, or AGENTS.md changed: want a refusal and the bytes unchanged", code)
				}
				return
			}
			if code != 0 {
				t.Fatalf("enable: exit %d, want the block left or updated", code)
			}
			if tc.kept && string(readBytes(t, piAgentsFile(dir))) != tc.text {
				t.Errorf("enable changed a current block:\n%q", readBytes(t, piAgentsFile(dir)))
			}
			if c := checkNamed(experimentalChecks("pi", dir), "instructions"); c == nil || c.status != "ok" {
				t.Errorf("instructions is %+v, want ok after enable", c)
			}
		})
	}
}

// TestDoctorExperimentalIsReadOnly checks that doctor for either client writes nothing: the tree of the project, and the
// files in it, are the same after the command as before.
func TestDoctorExperimentalIsReadOnly(t *testing.T) {
	for _, agent := range []string{"opencode", "pi"} {
		t.Run(agent, func(t *testing.T) {
			dir := agentProject(t)
			if code, _, stderr := runCmd(t, "enable", "--agent", agent); code != 0 {
				t.Fatalf("enable: exit %d, stderr %q", code, stderr)
			}
			before := treeOf(t, dir)
			var contents []string
			for _, p := range before {
				if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
					contents = append(contents, string(b))
				}
			}

			runCmd(t, "doctor", "--agent", agent)

			if after := treeOf(t, dir); !slices.Equal(after, before) {
				t.Errorf("doctor changed the tree: %v, was %v", after, before)
			}
			var after []string
			for _, p := range before {
				if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
					after = append(after, string(b))
				}
			}
			if !slices.Equal(after, contents) {
				t.Errorf("doctor changed a file")
			}
		})
	}
}

// TestDoctorExperimentalLinesAreTagged checks the section doctor adds for an experimental client: its header and every
// line after it carry the tag, so a line copied out of a terminal says the client is experimental.
func TestDoctorExperimentalLinesAreTagged(t *testing.T) {
	for _, agent := range []string{"opencode", "pi"} {
		t.Run(agent, func(t *testing.T) {
			agentProject(t)
			_, stdout, _ := runCmd(t, "doctor", "--agent", agent)

			header := "EXPERIMENTAL " + agent + ": registration for " + agent + ", read only\n"
			i := strings.Index(stdout, header)
			if i < 0 {
				t.Fatalf("no tagged header in:\n%s", stdout)
			}
			for _, line := range strings.Split(strings.TrimSuffix(stdout[i:], "\n"), "\n") {
				if !strings.HasPrefix(line, "EXPERIMENTAL "+agent+": ") {
					t.Errorf("line %q lacks the tag", line)
				}
			}
		})
	}
}

// TestDoctorExperimentalWithNoProject checks that a directory with no project file is skipped with the reason, not failed.
func TestDoctorExperimentalWithNoProject(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)

	checks := experimentalChecks("opencode", dir)
	if len(checks) != 1 || checks[0].status != "skip" || !strings.Contains(checks[0].detail, "could not be found") {
		t.Errorf("checks %v, want one skip that gives the reason", checks)
	}
	if anyFailed(checks) {
		t.Errorf("no project failed the check: %v", checks)
	}
}
