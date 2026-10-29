package identity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// file is a .cairn.toml (or any file) to write, at a path relative to the test root.
type file struct {
	path string
	body string
}

// tree builds a fixture: root is a fresh temporary directory, and the files
// are written under it. Every test sets CAIRN_PROJECT and CLAUDE_PROJECT_DIR
// itself, so the real environment is never read for a result.
type tree struct {
	root string
}

// newTree returns a tree rooted at a new t.TempDir() with the given files
// and a directory at work (relative to root), which always exists.
func newTree(t *testing.T, work string, files []file) tree {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f.path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, work), 0o755); err != nil {
		t.Fatal(err)
	}
	return tree{root: root}
}

// setEnv sets key to value for the test, or unsets it when value is "" and
// unset is true. t.Setenv restores the original value at cleanup, so a
// later Unsetenv is safe.
func setEnv(t *testing.T, key, value string, unset bool) {
	t.Helper()
	t.Setenv(key, value)
	if unset {
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

// TestResolve covers the resolution order. Two cases ("CLAUDE_PROJECT_DIR with
// no file ignores the working directory" and "neither env nor file") expect
// no file to be found, so the walk climbs from t.TempDir() to the filesystem
// root. That is a trade-off, recorded here: the walk only reads, but a stray
// .cairn.toml in an ancestor of the temp directory (/tmp, /var/folders, or
// $HOME on a developer machine or CI runner) would change those two results.
// Making the walk hermetic would need a seam used only by tests; none is added
// without a clearer need.
func TestResolve(t *testing.T) {
	const good = "project = \"acme-api\"\n"
	cases := []struct {
		name string
		// env is CAIRN_PROJECT. Empty means unset, unless envEmpty is set.
		env      string
		envEmpty bool
		// claudeDir is CLAUDE_PROJECT_DIR, relative to the tree root. Empty means unset.
		claudeDir string
		// work is the working directory, relative to the tree root.
		work  string
		files []file

		wantName   string
		wantSource Source
		// wantPath is the .cairn.toml path, relative to the tree root. Empty for SourceEnv.
		wantPath string
		// wantErr is "" for success, "none" for core.ErrNoProject, or a
		// core.Kind for any other error.
		wantErr string
		// wantText, when set, must appear in the error message.
		wantText string
	}{
		{
			name:       "env set, no file",
			env:        "acme-api",
			work:       "app",
			wantName:   "acme-api",
			wantSource: SourceEnv,
		},
		{
			name:       "env set beats the file in the working directory",
			env:        "from-env",
			work:       "app",
			files:      []file{{"app/.cairn.toml", good}},
			wantName:   "from-env",
			wantSource: SourceEnv,
		},
		{
			name:       "env empty is treated as unset, file used",
			envEmpty:   true,
			work:       "app",
			files:      []file{{"app/.cairn.toml", good}},
			wantName:   "acme-api",
			wantSource: SourceFile,
			wantPath:   "app/.cairn.toml",
		},
		{
			name:       "env unset, file in working directory",
			work:       "app",
			files:      []file{{"app/.cairn.toml", good}},
			wantName:   "acme-api",
			wantSource: SourceFile,
			wantPath:   "app/.cairn.toml",
		},
		{
			name:       "env unset, file in a parent directory",
			work:       "app/src/pkg",
			files:      []file{{".cairn.toml", good}},
			wantName:   "acme-api",
			wantSource: SourceFile,
			wantPath:   ".cairn.toml",
		},
		{
			name:       "nearest file wins over a parent file",
			work:       "app/src",
			files:      []file{{".cairn.toml", "project = \"outer\"\n"}, {"app/.cairn.toml", "project = \"inner\"\n"}},
			wantName:   "inner",
			wantSource: SourceFile,
			wantPath:   "app/.cairn.toml",
		},
		{
			name:       "CLAUDE_PROJECT_DIR is searched instead of the working directory",
			claudeDir:  "project",
			work:       "elsewhere",
			files:      []file{{"project/.cairn.toml", good}},
			wantName:   "acme-api",
			wantSource: SourceFile,
			wantPath:   "project/.cairn.toml",
		},
		{
			name:      "CLAUDE_PROJECT_DIR with no file ignores the working directory",
			claudeDir: "project",
			work:      "elsewhere",
			files:     []file{{"elsewhere/.cairn.toml", good}},
			wantErr:   "none",
		},
		{
			name:    "neither env nor file",
			work:    "app",
			wantErr: "none",
		},
		{
			name:     "malformed TOML",
			work:     "app",
			files:    []file{{"app/.cairn.toml", "project = acme-api\n"}},
			wantErr:  string(core.KindInvalid),
			wantText: "app/.cairn.toml is not valid TOML",
		},
		{
			name:     "file without a project key",
			work:     "app",
			files:    []file{{"app/.cairn.toml", "server = \"https://cairn.lan:8787\"\n"}},
			wantErr:  string(core.KindInvalid),
			wantText: "has no project key",
		},
		{
			name:     "file with an empty project key",
			work:     "app",
			files:    []file{{"app/.cairn.toml", "project = \"\"\n"}},
			wantErr:  string(core.KindInvalid),
			wantText: "has no project key",
		},
		{
			name:     "nearest file without a project key stops the walk",
			work:     "app",
			files:    []file{{".cairn.toml", good}, {"app/.cairn.toml", "server = \"x\"\n"}},
			wantErr:  string(core.KindInvalid),
			wantText: "has no project key",
		},
		{
			name:     "nearest file malformed stops the walk",
			work:     "app",
			files:    []file{{".cairn.toml", good}, {"app/.cairn.toml", "project = \n"}},
			wantErr:  string(core.KindInvalid),
			wantText: "not valid TOML",
		},
		{
			name:       "file with extra keys reads the project",
			work:       "app",
			files:      []file{{"app/.cairn.toml", "project = \"acme-api\"\n# server = \"https://cairn.lan:8787\"\nserver = \"https://cairn.lan:8787\"\n"}},
			wantName:   "acme-api",
			wantSource: SourceFile,
			wantPath:   "app/.cairn.toml",
		},
		{
			name:     "env set but invalid does not fall through to the file",
			env:      "Acme",
			work:     "app",
			files:    []file{{"app/.cairn.toml", good}},
			wantErr:  string(core.KindInvalid),
			wantText: "CAIRN_PROJECT",
		},
		{
			name:     "file project invalid",
			work:     "app",
			files:    []file{{"app/.cairn.toml", "project = \"acme_api\"\n"}},
			wantErr:  string(core.KindInvalid),
			wantText: "acme_api",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, tc.work, tc.files)
			setEnv(t, EnvProject, tc.env, tc.env == "" && !tc.envEmpty)
			claude := ""
			if tc.claudeDir != "" {
				claude = filepath.Join(tr.root, tc.claudeDir)
				if err := os.MkdirAll(claude, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			setEnv(t, EnvProjectDir, claude, claude == "")

			got, err := Resolve(filepath.Join(tr.root, tc.work))

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Resolve: got %+v, want an error", got)
				}
				if tc.wantErr == "none" {
					if !errors.Is(err, core.ErrNoProject) {
						t.Fatalf("Resolve error = %v, want core.ErrNoProject", err)
					}
					if !strings.Contains(err.Error(), "cairn init") {
						t.Errorf("error %q does not name `cairn init`", err)
					}
				} else if kind := core.KindOf(err); string(kind) != tc.wantErr {
					t.Errorf("Resolve error kind = %q (%v), want %q", kind, err, tc.wantErr)
				}
				if tc.wantText != "" && !strings.Contains(err.Error(), tc.wantText) {
					t.Errorf("error %q does not contain %q", err, tc.wantText)
				}
				return
			}

			if err != nil {
				t.Fatalf("Resolve: unexpected error %v", err)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tc.wantSource)
			}
			wantPath := ""
			if tc.wantPath != "" {
				wantPath = filepath.Join(tr.root, tc.wantPath)
			}
			if got.Path != wantPath {
				t.Errorf("Path = %q, want %q", got.Path, wantPath)
			}
		})
	}
}

// TestResolveNameRule checks the name rule of core.ValidProjectName from both
// sources. Each name must be refused as CAIRN_PROJECT and as a file's project key.
func TestResolveNameRule(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"acme-api", true},
		{"a", true},
		{"x1-2y", true},
		{"0", true},
		{strings.Repeat("a", 64), true},
		{"Acme", false},
		{"acme_api", false},
		{"-acme", false},
		{"acme-", false},
		{"acme--api", false},
		{"acme api", false},
		{"acme.api", false},
		{"acme:api", false},
		{strings.Repeat("a", 65), false},
	}
	for _, tc := range cases {
		t.Run("env "+tc.name, func(t *testing.T) {
			tr := newTree(t, "app", nil)
			setEnv(t, EnvProject, tc.name, false)
			setEnv(t, EnvProjectDir, "", true)
			got, err := Resolve(filepath.Join(tr.root, "app"))
			checkNameRule(t, tc.valid, tc.name, got, err)
		})
		t.Run("file "+tc.name, func(t *testing.T) {
			tr := newTree(t, "app", []file{{"app/.cairn.toml", "project = \"" + tc.name + "\"\n"}})
			setEnv(t, EnvProject, "", true)
			setEnv(t, EnvProjectDir, "", true)
			got, err := Resolve(filepath.Join(tr.root, "app"))
			checkNameRule(t, tc.valid, tc.name, got, err)
		})
	}
}

// checkNameRule asserts that a valid name resolves to itself and an invalid
// one is a KindInvalid error that names the rule.
func checkNameRule(t *testing.T, valid bool, name string, got Project, err error) {
	t.Helper()
	if valid {
		if err != nil {
			t.Fatalf("name %q: unexpected error %v", name, err)
		}
		if got.Name != name {
			t.Errorf("name %q: Name = %q", name, got.Name)
		}
		return
	}
	if err == nil {
		t.Fatalf("name %q: got %+v, want an error", name, got)
	}
	if kind := core.KindOf(err); kind != core.KindInvalid {
		t.Errorf("name %q: kind = %q (%v), want invalid", name, kind, err)
	}
	if !strings.Contains(err.Error(), "lowercase letters") {
		t.Errorf("name %q: error %q does not state the rule", name, err)
	}
}
