package client_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/client"
)

// TestASymlinkIsJudgedByItsTarget checks 8-S1: a client.env that is a link is read and refused
// by the mode of the file it names. A link to a file any other user can read is refused, and a
// link to an owner-only file is accepted. The refusal names the link's path, not the target's key.
func TestASymlinkIsJudgedByItsTarget(t *testing.T) {
	key := newKey(t)
	home := keyEnv(t, "")
	dir := filepath.Join(home, ".config", "cairn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "client.env")

	open := filepath.Join(home, "open.env")
	if err := os.WriteFile(open, []byte("CAIRN_API_KEY="+key+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(open, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	k, err := client.LoadKey()
	if err == nil || k.Value != "" {
		t.Errorf("a link to a 0644 file: LoadKey = %v, %v; want it refused", k, err)
	}
	if err != nil && strings.Contains(err.Error(), key) {
		t.Errorf("the refusal prints the key: %v", err)
	}

	owner := filepath.Join(home, "owner.env")
	if err := os.WriteFile(owner, []byte("CAIRN_API_KEY="+key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(owner, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(owner, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	k, err = client.LoadKey()
	if err != nil || k.Value != key || k.Source != client.SourceFile {
		t.Errorf("a link to a 0600 file: LoadKey = %v from %s, %v; want the key from the file", k, k.Source, err)
	}
}

// TestAnExportLineIsAccepted checks 8-S2: a client.env line that begins with "export " is read as
// the CAIRN_API_KEY line it is, as a shell would read it, and not skipped.
func TestAnExportLineIsAccepted(t *testing.T) {
	key := newKey(t)
	home := keyEnv(t, "")
	writeKeyFile(t, home, 0o600, "export CAIRN_API_KEY="+key+"\n")
	k, err := client.LoadKey()
	if err != nil || k.Value != key {
		t.Errorf("an export line: LoadKey = %v, %v; want the key", k, err)
	}
}

// TestSendsKeyInClear checks the rule behind the doctor's transport warning: a key is in clear text
// when the URL is http and the host is not loopback. https is never in clear text, and localhost
// and loopback addresses are not sent over a network.
func TestSendsKeyInClear(t *testing.T) {
	cases := map[string]bool{
		"http://cairn.example.invalid:8788":     true,
		"http://192.168.1.5:8788":               true,
		"https://cairn.example.invalid":         false,
		"http://127.0.0.1:8788":                 false,
		"http://localhost:8788":                 false,
		"http://LOCALHOST:8788":                 false,
		"http://localhost.:8788":                false,
		"http://app.localhost:8788":             false,
		"http://localhost.example.invalid:8788": true,
		"http://[::1]:8788":                     false,
	}
	for base, want := range cases {
		if got := client.SendsKeyInClear(base); got != want {
			t.Errorf("SendsKeyInClear(%q) = %v, want %v", base, got, want)
		}
	}
}
