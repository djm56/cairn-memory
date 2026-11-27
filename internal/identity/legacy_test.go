package identity

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLegacyNames checks the fallbacks kept from before the rename to Cairnbook: an old
// CAIRN_ variable is read only when the new one is unset or empty, and an old .cairn.toml
// is read only in a directory that has no .cairnbook.toml.
func TestLegacyNames(t *testing.T) {
	t.Run("old project variable", func(t *testing.T) {
		tr := newTree(t, "work", nil)
		setEnv(t, EnvProjectDir, "", true)
		setEnv(t, EnvProject, "", true)
		t.Setenv("CAIRN_PROJECT", "old-name")
		p, err := Resolve(filepath.Join(tr.root, "work"))
		if err != nil || p.Name != "old-name" || p.Source != SourceEnv {
			t.Fatalf("Resolve = %+v, %v; want old-name from env", p, err)
		}
	})
	t.Run("new variable wins", func(t *testing.T) {
		t.Setenv(EnvProject, "new-name")
		t.Setenv("CAIRN_PROJECT", "old-name")
		if v, from := Lookup(EnvProject); v != "new-name" || from != EnvProject {
			t.Fatalf("Lookup = %q from %q, want new-name from %s", v, from, EnvProject)
		}
	})
	t.Run("old variable named in a refusal", func(t *testing.T) {
		setEnv(t, EnvProject, "", true)
		t.Setenv("CAIRN_PROJECT", "Not Valid")
		_, err := Resolve(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "CAIRN_PROJECT") {
			t.Fatalf("Resolve error = %v, want one naming CAIRN_PROJECT", err)
		}
	})
	t.Run("old server variable", func(t *testing.T) {
		setEnv(t, EnvServer, "", true)
		t.Setenv("CAIRN_SERVER", "https://old.example")
		if got, err := ResolveServer(t.TempDir()); err != nil || got != "https://old.example" {
			t.Fatalf("ResolveServer = %q, %v", got, err)
		}
	})
	t.Run("old project file", func(t *testing.T) {
		tr := newTree(t, "a/b", []file{{path: LegacyFileName, body: "project = \"old-file\"\n"}})
		setEnv(t, EnvProjectDir, "", true)
		setEnv(t, EnvProject, "", true)
		p, err := Resolve(filepath.Join(tr.root, "a/b"))
		if err != nil || p.Name != "old-file" || filepath.Base(p.Path) != LegacyFileName {
			t.Fatalf("Resolve = %+v, %v; want old-file from %s", p, err, LegacyFileName)
		}
	})
	t.Run("new file wins in the same directory", func(t *testing.T) {
		tr := newTree(t, "", []file{
			{path: LegacyFileName, body: "project = \"old-file\"\n"},
			{path: FileName, body: "project = \"new-file\"\n"},
		})
		setEnv(t, EnvProjectDir, "", true)
		setEnv(t, EnvProject, "", true)
		p, err := Resolve(tr.root)
		if err != nil || p.Name != "new-file" {
			t.Fatalf("Resolve = %+v, %v; want new-file", p, err)
		}
	})
	t.Run("nearer old file wins over a farther new one", func(t *testing.T) {
		tr := newTree(t, "sub", []file{
			{path: FileName, body: "project = \"outer\"\n"},
			{path: "sub/" + LegacyFileName, body: "project = \"inner\"\n"},
		})
		setEnv(t, EnvProjectDir, "", true)
		setEnv(t, EnvProject, "", true)
		p, err := Resolve(filepath.Join(tr.root, "sub"))
		if err != nil || p.Name != "inner" {
			t.Fatalf("Resolve = %+v, %v; want inner", p, err)
		}
	})
}

// TestServerSourceNamesTheFileRead checks that ServerSource names the file ResolveServer reads: the old
// .cairn.toml when it is the nearest file, the new name when the directory has it, and the variable when the
// environment gives the server. It is what status and doctor print after "from".
func TestServerSourceNamesTheFileRead(t *testing.T) {
	t.Run("old file is the one read", func(t *testing.T) {
		tr := newTree(t, "", []file{{path: LegacyFileName, body: "server = \"https://old.example\"\n"}})
		setEnv(t, EnvServer, "", true)
		setEnv(t, EnvProjectDir, "", true)
		if got := ServerSource(tr.root); got != LegacyFileName {
			t.Errorf("ServerSource = %q, want %q", got, LegacyFileName)
		}
	})
	t.Run("new file wins in the same directory", func(t *testing.T) {
		tr := newTree(t, "", []file{
			{path: LegacyFileName, body: "server = \"https://old.example\"\n"},
			{path: FileName, body: "server = \"https://new.example\"\n"},
		})
		setEnv(t, EnvServer, "", true)
		setEnv(t, EnvProjectDir, "", true)
		if got := ServerSource(tr.root); got != FileName {
			t.Errorf("ServerSource = %q, want %q", got, FileName)
		}
	})
	t.Run("environment names itself", func(t *testing.T) {
		setEnv(t, EnvServer, "", true)
		t.Setenv("CAIRN_SERVER", "https://old.example")
		if got := ServerSource(t.TempDir()); got != "CAIRN_SERVER" {
			t.Errorf("ServerSource = %q, want CAIRN_SERVER", got)
		}
	})
}

// TestEmptyNewNamesFallBackToTheOld checks that a new variable set to "" counts as unset, so the old
// name is read: an empty CAIRNBOOK_PROJECT gives the project from CAIRN_PROJECT, and an empty
// CAIRNBOOK_SERVER gives the server from CAIRN_SERVER, which ServerSource names.
func TestEmptyNewNamesFallBackToTheOld(t *testing.T) {
	t.Run("project", func(t *testing.T) {
		setEnv(t, EnvProjectDir, "", true)
		setEnv(t, EnvProject, "", false)
		t.Setenv("CAIRN_PROJECT", "old-name")
		p, err := Resolve(t.TempDir())
		if err != nil || p.Name != "old-name" || p.Source != SourceEnv {
			t.Fatalf("Resolve = %+v, %v; want old-name from env", p, err)
		}
	})
	t.Run("server", func(t *testing.T) {
		setEnv(t, EnvServer, "", false)
		setEnv(t, EnvProjectDir, "", true)
		t.Setenv("CAIRN_SERVER", "https://old.example")
		dir := t.TempDir()
		if got, err := ResolveServer(dir); err != nil || got != "https://old.example" {
			t.Fatalf("ResolveServer = %q, %v; want the old variable's server", got, err)
		}
		if got := ServerSource(dir); got != "CAIRN_SERVER" {
			t.Errorf("ServerSource = %q, want CAIRN_SERVER", got)
		}
	})
}
