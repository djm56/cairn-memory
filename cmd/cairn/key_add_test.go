package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/server"
)

// failWriter is a writer that refuses every write, as a closed pipe or a full disk does.
type failWriter struct{}

// Write refuses the bytes.
func (failWriter) Write([]byte) (int, error) { return 0, errors.New("the pipe is closed") }

// TestKeyAddRevokesAKeyItCannotShow checks 8-W7: when the key cannot be written to stdout, add
// revokes the key it just made, exits 1, and says in words that the key was made and revoked. No
// key text reaches stderr, and the key is revoked in the database, not left live and unseen.
func TestKeyAddRevokesAKeyItCannotShow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	var stderr bytes.Buffer
	code := runKey([]string{"add", "--db", db, "--name", "ci", "--scope", "agent"}, failWriter{}, &stderr)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "named ci") || !strings.Contains(stderr.String(), "so it was revoked") {
		t.Errorf("stderr %q does not name the key and say it was revoked", stderr.String())
	}
	if strings.Contains(stderr.String(), auth.KeyPrefix) {
		t.Errorf("stderr prints key text: %q", stderr.String())
	}
	keys, err := server.ListKeys(context.Background(), db)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "ci" || keys[0].RevokedAt == nil {
		t.Errorf("keys after the failed add = %+v, want the one key ci, revoked", keys)
	}
}

// TestKeyAddRevokesWhenTheStdoutPipeIsClosed is 10-W3. The Go runtime ends a process with
// SIGPIPE only for a failed write to fd 1 or 2, so an in-process write to an os.Pipe proves
// nothing. The test runs key add in a child process whose stdout is a pipe with no reader. The
// child must answer through the revoke branch: exit 1, the key revoked. Without the SIGPIPE
// ignore the child dies from the signal, and the parent sees no exit code 1.
func TestKeyAddRevokesWhenTheStdoutPipeIsClosed(t *testing.T) {
	if os.Getenv(keyAddHelperEnv) == "1" {
		// The child: stdout is the pipe with no reader.
		os.Exit(runKey([]string{"add", "--db", os.Getenv(keyAddHelperDB), "--name", "piped", "--scope", "agent"}, os.Stdout, os.Stderr))
	}
	db := filepath.Join(t.TempDir(), "cairn.db")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close the reader: %v", err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestKeyAddRevokesWhenTheStdoutPipeIsClosed$")
	cmd.Env = append(os.Environ(), keyAddHelperEnv+"=1", keyAddHelperDB+"="+db)
	cmd.Stdout = w
	cmd.Stderr = &stderr
	err = cmd.Run()
	w.Close()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("child: %v (stderr %q), want exit 1", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "so it was revoked") {
		t.Errorf("stderr %q does not say the key was revoked", stderr.String())
	}
	keys, lerr := server.ListKeys(context.Background(), db)
	if lerr != nil {
		t.Fatalf("ListKeys: %v", lerr)
	}
	if len(keys) != 1 || keys[0].Name != "piped" || keys[0].RevokedAt == nil {
		t.Errorf("keys after the closed-pipe add = %+v, want the one key piped, revoked", keys)
	}
}

const (
	keyAddHelperEnv = "CAIRN_KEYADD_CHILD"
	keyAddHelperDB  = "CAIRN_KEYADD_DB"
)

// TestKeyAddNamesTheDatabaseItCreates checks 8-S4: a key add that creates the database says so
// on stderr, and one that finds it does not.
func TestKeyAddNamesTheDatabaseItCreates(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	code, _, errs := runCmd(t, "key", "add", "--db", db, "--name", "ci", "--scope", "agent")
	if code != 0 || !strings.Contains(errs, "cairn key add: created "+db) {
		t.Errorf("first add: exit %d, stderr %q; want the created line naming %s", code, errs, db)
	}
	code, _, errs = runCmd(t, "key", "add", "--db", db, "--name", "ops", "--scope", "operator")
	if code != 0 || strings.Contains(errs, "created") {
		t.Errorf("second add: exit %d, stderr %q; want no created line", code, errs)
	}
}

// TestKeyAddMakesTheDatabaseOwnerOnly checks 8-S9 at the command: the database that cairn key add
// creates is 0600, not only the database the server opens.
func TestKeyAddMakesTheDatabaseOwnerOnly(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	if code, _, errs := runCmd(t, "key", "add", "--db", db, "--name", "ci", "--scope", "agent"); code != 0 {
		t.Fatalf("key add: exit %d, stderr %q", code, errs)
	}
	info, err := os.Stat(db)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the database is mode %04o, want 0600", mode)
	}
}

// TestKeyRevokeDoesNotPrintAPastedKey checks 8-W6 at the command: revoking with a key given as
// the name is exit 1, prints the rule, and prints no key, on either stream.
func TestKeyRevokeDoesNotPrintAPastedKey(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	code, out, errs := runCmd(t, "key", "add", "--db", db, "--name", "ci", "--scope", "agent")
	if code != 0 {
		t.Fatalf("key add: exit %d, stderr %q", code, errs)
	}
	key := strings.TrimSpace(out)
	code, out, errs = runCmd(t, "key", "revoke", "--db", db, "--name", key)
	if code != 1 || !strings.Contains(errs, "must not begin with cairn_") {
		t.Errorf("revoke with a key as the name: exit %d, stderr %q; want 1 and the name rule", code, errs)
	}
	if strings.Contains(out, key) || strings.Contains(errs, key) {
		t.Errorf("revoke printed the key: stdout %q, stderr %q", out, errs)
	}
}
