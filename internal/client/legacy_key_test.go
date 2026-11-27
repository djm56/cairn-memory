package client_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/client"
)

// writeLegacyKeyFile writes the key file at its path from before the rename to Cairnbook,
// ~/.config/cairn/client.env, owner-only.
func writeLegacyKeyFile(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "cairn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLegacyKeyNames checks the fallbacks kept from before the rename: CAIRN_API_KEY in the
// environment, the old key file when there is no new one, and a CAIRN_API_KEY line in a file
// with no CAIRNBOOK_API_KEY line. The new name wins wherever both are given.
func TestLegacyKeyNames(t *testing.T) {
	t.Run("old variable", func(t *testing.T) {
		want := newKey(t)
		keyEnv(t, "")
		t.Setenv(client.LegacyEnvKey, want)
		k, err := client.LoadKey()
		if err != nil || k.Value != want || k.Source != client.SourceEnv {
			t.Fatalf("LoadKey = %v, %v; want the old variable's key from env", k, err)
		}
	})
	t.Run("new variable wins", func(t *testing.T) {
		want := newKey(t)
		keyEnv(t, want)
		t.Setenv(client.LegacyEnvKey, newKey(t))
		if k, err := client.LoadKey(); err != nil || k.Value != want {
			t.Fatalf("LoadKey = %v, %v; want the new variable's key", k, err)
		}
	})
	t.Run("old file when there is no new one", func(t *testing.T) {
		want := newKey(t)
		home := keyEnv(t, "")
		writeLegacyKeyFile(t, home, "CAIRN_API_KEY="+want+"\n")
		k, err := client.LoadKey()
		if err != nil || k.Value != want || k.Source != client.SourceFile {
			t.Fatalf("LoadKey = %v, %v; want the old file's key", k, err)
		}
	})
	t.Run("new file wins over the old one", func(t *testing.T) {
		want := newKey(t)
		home := keyEnv(t, "")
		writeLegacyKeyFile(t, home, "CAIRN_API_KEY="+newKey(t)+"\n")
		writeKeyFile(t, home, 0o600, "CAIRNBOOK_API_KEY="+want+"\n")
		if k, err := client.LoadKey(); err != nil || k.Value != want {
			t.Fatalf("LoadKey = %v, %v; want the new file's key", k, err)
		}
	})
	t.Run("new line wins over an old line in one file", func(t *testing.T) {
		want := newKey(t)
		home := keyEnv(t, "")
		writeKeyFile(t, home, 0o600, "CAIRNBOOK_API_KEY="+want+"\nCAIRN_API_KEY="+newKey(t)+"\n")
		if k, err := client.LoadKey(); err != nil || k.Value != want {
			t.Fatalf("LoadKey = %v, %v; want the CAIRNBOOK_API_KEY line", k, err)
		}
	})
}

// TestLegacyKeyFileRules checks the rules a key file meets on the old path as on the new one, and the
// point at which the old file is not read: a new path that cannot be looked at is an error, not a
// fallback; a new file with no key blocks the old one; the old file at 0644 is refused; and of several
// CAIRNBOOK_API_KEY lines in one file the last wins. Each case points HOME at its own directory.
func TestLegacyKeyFileRules(t *testing.T) {
	t.Run("a new path that cannot be read is an error, not a fallback", func(t *testing.T) {
		home := keyEnv(t, "")
		writeLegacyKeyFile(t, home, "CAIRN_API_KEY="+newKey(t)+"\n")
		// A regular file where the new directory should be: its entry is ENOTDIR, not ENOENT.
		if err := os.WriteFile(filepath.Join(home, ".config", "cairnbook"), []byte("not a directory\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		k, err := client.LoadKey()
		if err == nil || k.Value != "" {
			t.Fatalf("LoadKey = %v, %v; want an error and no key from the old file", k, err)
		}
	})
	t.Run("a new file with no key blocks the old file", func(t *testing.T) {
		home := keyEnv(t, "")
		writeKeyFile(t, home, 0o600, "# no key here\n")
		writeLegacyKeyFile(t, home, "CAIRN_API_KEY="+newKey(t)+"\n")
		k, err := client.LoadKey()
		if err != nil || k.Value != "" || k.Source != "" {
			t.Fatalf("LoadKey = %v, %v; want no key, and the old file not read", k, err)
		}
	})
	t.Run("the old file is refused at mode 0644", func(t *testing.T) {
		home := keyEnv(t, "")
		dir := filepath.Join(home, ".config", "cairn")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "client.env")
		if err := os.WriteFile(path, []byte("CAIRN_API_KEY="+newKey(t)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		k, err := client.LoadKey()
		if err == nil || !strings.Contains(err.Error(), "open to other users") || k.Value != "" {
			t.Fatalf("LoadKey = %v, %v; want the mode refusal and no key", k, err)
		}
	})
	t.Run("the last CAIRNBOOK_API_KEY line wins", func(t *testing.T) {
		home := keyEnv(t, "")
		want := newKey(t)
		writeKeyFile(t, home, 0o600, "CAIRNBOOK_API_KEY="+newKey(t)+"\nCAIRNBOOK_API_KEY="+want+"\n")
		if k, err := client.LoadKey(); err != nil || k.Value != want {
			t.Fatalf("LoadKey = %v, %v; want the last line's key", k, err)
		}
	})
}
