//go:build unix

package client_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/client"
)

// TestANamedPipeAsTheKeyFileIsRefusedWithoutBlocking checks 8-S1: a client.env that is a named
// pipe is refused as not a regular file, and the lookup does not block on it. The open is
// non-blocking and the kind is checked on the descriptor (step 10 review 10-S3), so the test
// fails rather than hangs if the open waits for a writer.
//
// This test does not prove the swap race (a regular file replaced by a FIFO between a check and
// the open). The FIFO exists from the start, so it would pass against a stat-then-open lookup
// too. Building the swap needs a hook between the two calls, which the code does not have; the
// race is closed by the design (no check precedes the open), not by this test.
func TestANamedPipeAsTheKeyFileIsRefusedWithoutBlocking(t *testing.T) {
	home := keyEnv(t, "")
	dir := filepath.Join(home, ".config", "cairn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(dir, "client.env")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("this platform cannot make a named pipe: %v", err)
	}
	type result struct {
		err error
		key string
	}
	done := make(chan result, 1)
	go func() {
		k, err := client.LoadKey()
		done <- result{err: err, key: k.Value}
	}()
	select {
	case r := <-done:
		if r.err == nil || !strings.Contains(r.err.Error(), "not a regular file") || r.key != "" {
			t.Errorf("a named pipe: err = %v, key %q; want the not-a-regular-file refusal", r.err, r.key)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadKey blocked on a named pipe: the open must not wait for a writer")
	}
}
