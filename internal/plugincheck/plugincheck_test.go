// Package plugincheck tests the Claude Code plugin under plugins/claude-code and the marketplace file at the repository
// root. The launcher is run by sh against stub cairn binaries, with its PATH and HOME set by the test, so the tests need no
// install. The manifest, the MCP file and the command files are read as the text they are; nothing here runs claude.
// `claude plugin validate` is a separate check, run by hand and in CI.
package plugincheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// pluginDir is the plugin, relative to this package. launcher is its launcher, the only file the MCP entry and the hooks
// run.
const (
	pluginDir = "../../plugins/claude-code"
	launcher  = pluginDir + "/bin/cairn-launch"
	repoRoot  = "../.."
)

// installLine is the install command the launcher prints, and docs/QUICK-INSTALL.md gives in pieces.
const installLine = "install -m 0755 bin/cairn ~/.local/bin/cairn"

// stubCairn is a file named cairn that prints which stub ran, then each argument on its own line. It uses only shell
// builtins, so it runs with the test's PATH.
func stubCairn(label string) string {
	return "#!/bin/sh\necho \"stub: " + label + "\"\nfor a in \"$@\"; do echo \"arg: $a\"; done\n"
}

// writeExecutable writes content to dir/name with the executable bit set, and returns the path.
func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// launcherPath is the absolute path of the launcher, which the tests run and link to.
func launcherPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(launcher)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// runLauncher runs the launcher with args, from dir, with exactly the environment env. It returns the exit code, stdout
// and stderr. A run that takes longer than 20 seconds is killed and fails the test, so a launcher that runs itself cannot
// hang the suite.
func runLauncher(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, launcherPath(t), args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("the launcher did not finish within 20 seconds, stderr %q", stderr.String())
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run the launcher: %v", err)
	}
	return code, stdout.String(), stderr.String()
}

// skipOnWindows skips a test that runs the launcher, which is a sh script.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the launcher is a sh script, and there is no sh to run it here")
	}
}

// TestLauncherIsExecutable checks the mode of the launcher. A plugin's bin/ files run as bare commands only when they are
// executable, and the git mode must carry the bit too.
func TestLauncherIsExecutable(t *testing.T) {
	skipOnWindows(t)
	info, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("%s is mode %v, want executable", launcher, info.Mode())
	}
}

// TestLauncherRunsCairnFromPath checks that a cairn on PATH is run, ahead of one in ~/.local/bin, with every argument passed
// through unchanged.
func TestLauncherRunsCairnFromPath(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	home := t.TempDir()
	writeExecutable(t, bin, "cairn", stubCairn("path"))
	writeExecutable(t, filepath.Join(home, ".local", "bin"), "cairn", stubCairn("home"))

	code, stdout, stderr := runLauncher(t, t.TempDir(), []string{"PATH=" + bin, "HOME=" + home},
		"mcp", "a b", "-x", "--flag=$HOME", "")
	want := "stub: path\narg: mcp\narg: a b\narg: -x\narg: --flag=$HOME\narg: \n"
	if code != 0 || stderr != "" || stdout != want {
		t.Errorf("exit %d, stdout %q, stderr %q: want 0, %q, and nothing on stderr", code, stdout, stderr, want)
	}
}

// TestLauncherFallsBackToHomeLocalBin checks that a cairn only in ~/.local/bin is run when PATH has none.
func TestLauncherFallsBackToHomeLocalBin(t *testing.T) {
	skipOnWindows(t)
	home := t.TempDir()
	writeExecutable(t, filepath.Join(home, ".local", "bin"), "cairn", stubCairn("home"))

	code, stdout, stderr := runLauncher(t, t.TempDir(), []string{"PATH=" + t.TempDir(), "HOME=" + home}, "status")
	if code != 0 || stderr != "" || stdout != "stub: home\narg: status\n" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 0 and the home copy run", code, stdout, stderr)
	}
}

// TestLauncherNotFoundNamesTheInstall checks the case with no cairn anywhere: exit 127, nothing on stdout, and one line on
// stderr that names the install command from docs/QUICK-INSTALL.md.
func TestLauncherNotFoundNamesTheInstall(t *testing.T) {
	skipOnWindows(t)
	code, stdout, stderr := runLauncher(t, t.TempDir(), []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}, "mcp")
	if code != 127 {
		t.Errorf("exit %d, want 127, the status for a command that cannot be found", code)
	}
	if stdout != "" {
		t.Errorf("stdout %q, want nothing", stdout)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr %q, want exactly one line", stderr)
	}
	for _, want := range []string{"cairn-launch: cairn is not on PATH or in ~/.local/bin.", "make build", installLine, "docs/QUICK-INSTALL.md"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q lacks %q", stderr, want)
		}
	}
}

// TestLauncherNotFoundWithNoHome checks that a launcher with no HOME, or a relative one, does not search a home directory,
// and reports the same not-found line.
func TestLauncherNotFoundWithNoHome(t *testing.T) {
	skipOnWindows(t)
	rel := t.TempDir()
	// A decoy in the directory the relative HOME names, so a launcher that searched it would run the decoy.
	writeExecutable(t, filepath.Join(rel, "relative-home", ".local", "bin"), "cairn", stubCairn("relative home"))
	for _, home := range []string{"", "relative-home"} {
		code, stdout, _ := runLauncher(t, rel, []string{"PATH=" + t.TempDir(), "HOME=" + home}, "mcp")
		if code != 127 || stdout != "" {
			t.Errorf("HOME=%q: exit %d, stdout %q, want 127 and nothing run", home, code, stdout)
		}
	}
}

// TestLauncherSkipsEmptyAndRelativePathEntries checks that the launcher searches only absolute directories. A cairn in the
// directory a session runs from is never run, through an empty PATH entry or a relative one.
func TestLauncherSkipsEmptyAndRelativePathEntries(t *testing.T) {
	skipOnWindows(t)
	cwd := t.TempDir()
	writeExecutable(t, cwd, "cairn", stubCairn("cwd"))
	writeExecutable(t, filepath.Join(cwd, "rel"), "cairn", stubCairn("relative"))

	for _, path := range []string{":rel", "rel:", "::rel", "."} {
		code, stdout, _ := runLauncher(t, cwd, []string{"PATH=" + path, "HOME=" + t.TempDir()}, "mcp")
		if code != 127 || stdout != "" {
			t.Errorf("PATH=%q: exit %d, stdout %q, want 127 and nothing run from the working directory", path, code, stdout)
		}
	}
}

// TestLauncherDoesNotGlobPathEntries checks that a PATH entry holding *, ? or [ is searched as the literal directory it names.
// Each case puts a decoy directory, xy, beside the entry: the pattern matches it, and it holds a cairn. The literal entry
// holds no cairn, so a launcher that expanded the pattern would run the decoy, and the right result is not found (127). A
// second case holds a cairn in the literal entry, which must run.
func TestLauncherDoesNotGlobPathEntries(t *testing.T) {
	skipOnWindows(t)
	for _, literal := range []string{"x*", "x?", "x[y]"} {
		t.Run(literal, func(t *testing.T) {
			bin := t.TempDir()
			home := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "xy"), "cairn", stubCairn("decoy"))
			entry := filepath.Join(bin, literal)
			if err := os.MkdirAll(entry, 0o755); err != nil {
				t.Fatal(err)
			}

			code, stdout, _ := runLauncher(t, t.TempDir(), []string{"PATH=" + entry, "HOME=" + home}, "mcp")
			if code != 127 || stdout != "" {
				t.Errorf("PATH entry %q: exit %d, stdout %q, want 127 and the decoy not run", literal, code, stdout)
			}

			writeExecutable(t, entry, "cairn", stubCairn("literal"))
			code, stdout, stderr := runLauncher(t, t.TempDir(), []string{"PATH=" + entry, "HOME=" + home}, "mcp")
			if code != 0 || stderr != "" || stdout != "stub: literal\narg: mcp\n" {
				t.Errorf("PATH entry %q: exit %d, stdout %q, stderr %q: want the literal directory's cairn run", literal, code, stdout, stderr)
			}
		})
	}
}

// TestLauncherSkipsItself checks the guard: a cairn on PATH that is a link to this launcher is skipped, so the launcher does
// not run itself, and the copy in ~/.local/bin is run instead.
func TestLauncherSkipsItself(t *testing.T) {
	skipOnWindows(t)
	bin := t.TempDir()
	if err := os.Symlink(launcherPath(t), filepath.Join(bin, "cairn")); err != nil {
		t.Skipf("this system cannot make a symbolic link: %v", err)
	}
	home := t.TempDir()
	writeExecutable(t, filepath.Join(home, ".local", "bin"), "cairn", stubCairn("home"))

	code, stdout, stderr := runLauncher(t, t.TempDir(), []string{"PATH=" + bin, "HOME=" + home}, "mcp")
	if code != 0 || stderr != "" || stdout != "stub: home\narg: mcp\n" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 0 and the home copy run, not the launcher", code, stdout, stderr)
	}
}

// TestLauncherInstallLineIsInQuickInstall checks that the install command the launcher prints is the one docs/QUICK-INSTALL.md
// gives, so the two cannot drift apart.
func TestLauncherInstallLineIsInQuickInstall(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "QUICK-INSTALL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"make build", installLine} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("docs/QUICK-INSTALL.md lacks %q, which the launcher's not-found line names", want)
		}
	}
}

// mcpFile is the shape of the plugin's .mcp.json: a map of server names under mcpServers.
type mcpFile struct {
	McpServers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"mcpServers"`
}

// TestMCPConfigRunsTheLauncher checks that the plugin's MCP server is cairn, started by the launcher with the mcp argument,
// and that the launcher the entry names exists in the plugin.
func TestMCPConfigRunsTheLauncher(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(pluginDir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc mcpFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf(".mcp.json is not JSON: %v", err)
	}
	server, ok := doc.McpServers["cairn"]
	if len(doc.McpServers) != 1 || !ok {
		t.Fatalf(".mcp.json servers %v, want exactly one named cairn", doc.McpServers)
	}
	if server.Command != "${CLAUDE_PLUGIN_ROOT}/bin/cairn-launch" || !slices.Equal(server.Args, []string{"mcp"}) {
		t.Errorf("cairn server is %q %v, want the launcher with the mcp argument", server.Command, server.Args)
	}
	if _, err := os.Stat(launcher); err != nil {
		t.Errorf("the launcher the entry names is missing: %v", err)
	}
}

// manifest is the part of plugin.json the tests read.
type manifest struct {
	Name string `json:"name"`
}

// TestPluginManifestHasNoUserConfig checks that plugin.json names the plugin cairn and declares no userConfig at all: the key
// comes from CAIRN_API_KEY or client.env, and the server from .cairn.toml (D36).
func TestPluginManifestHasNoUserConfig(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("plugin.json is not JSON: %v", err)
	}
	if m.Name != "cairn" {
		t.Errorf("name is %q, want cairn", m.Name)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["userConfig"]; ok || bytes.Contains(raw, []byte("userConfig")) {
		t.Errorf("plugin.json declares userConfig, which D36 removes")
	}
}

// marketplace is the part of the root marketplace file the tests read.
type marketplace struct {
	Name  string `json:"name"`
	Owner struct {
		Name string `json:"name"`
	} `json:"owner"`
	Plugins []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"plugins"`
}

// TestMarketplaceListsThePlugin checks the root marketplace: named cairn-memory, with an owner, and one plugin whose name is
// the manifest's name and whose source is the plugin directory, which holds a manifest.
func TestMarketplaceListsThePlugin(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mp marketplace
	if err := json.Unmarshal(raw, &mp); err != nil {
		t.Fatalf("marketplace.json is not JSON: %v", err)
	}
	if mp.Name != "cairn-memory" || mp.Owner.Name == "" {
		t.Errorf("marketplace is %q with owner %q, want cairn-memory with an owner", mp.Name, mp.Owner.Name)
	}
	if len(mp.Plugins) != 1 {
		t.Fatalf("marketplace lists %d plugins, want one", len(mp.Plugins))
	}
	entry := mp.Plugins[0]
	if entry.Name != "cairn" || entry.Source != "./plugins/claude-code" {
		t.Errorf("plugin entry is %q from %q, want cairn from ./plugins/claude-code", entry.Name, entry.Source)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(entry.Source), ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("the entry's source holds no plugin.json: %v", err)
	}
}

// bashPattern finds each Bash(...) rule in an allowed-tools line.
var bashPattern = regexp.MustCompile(`Bash\(([^)]*)\)`)

// TestCommandsRunOnlyTheLauncher checks the two slash commands: each has a description, and each allows only the launcher's
// Bash invocation, and its body runs the matching cairn verb through the launcher.
func TestCommandsRunOnlyTheLauncher(t *testing.T) {
	cases := map[string]string{
		"init.md":   "cairn-launch init",
		"status.md": "cairn-launch status",
	}
	for file, verb := range cases {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(pluginDir, "commands", file))
			if err != nil {
				t.Fatal(err)
			}
			front, body, ok := splitFrontmatter(string(raw))
			if !ok {
				t.Fatalf("%s has no frontmatter between --- lines", file)
			}
			if !hasKey(front, "description") {
				t.Errorf("%s has no description", file)
			}
			var rules []string
			for _, m := range bashPattern.FindAllStringSubmatch(front, -1) {
				rules = append(rules, m[1])
			}
			if len(rules) == 0 {
				t.Fatalf("%s allows no Bash invocation", file)
			}
			for _, rule := range rules {
				if !strings.HasPrefix(rule, "cairn-launch ") {
					t.Errorf("%s allows Bash(%s), want only the launcher", file, rule)
				}
			}
			if !strings.Contains(body, verb) {
				t.Errorf("%s body lacks %q", file, verb)
			}
		})
	}
}

// hasKey reports whether the frontmatter text front has a line that starts with key and a colon.
func hasKey(front, key string) bool {
	for _, line := range strings.Split(front, "\n") {
		if strings.HasPrefix(line, key+":") {
			return true
		}
	}
	return false
}

// splitFrontmatter returns the text between the leading --- lines of a Markdown file, and the text after them.
func splitFrontmatter(s string) (front, body string, ok bool) {
	rest, found := strings.CutPrefix(s, "---\n")
	if !found {
		return "", "", false
	}
	front, body, found = strings.Cut(rest, "\n---\n")
	return front, body, found
}
