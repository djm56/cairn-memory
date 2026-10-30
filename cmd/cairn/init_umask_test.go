//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestInitLeavesTheUmaskInCharge is step 12 review W4. The file is created at 0644 and the
// umask takes its share, on the link path and on the direct write of the fallback. A
// umask of 077 leaves 0600; the file is not chmodded past it.
func TestInitLeavesTheUmaskInCharge(t *testing.T) {
	prev := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(prev) })

	for name, fallback := range map[string]bool{"link": false, "fallback": true} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			useDir(t, dir)
			if fallback {
				stubLink(t, syscall.EPERM)
			}
			if code, _, stderr := runCmd(t, initArgs(initProject, initServer)...); code != 0 {
				t.Fatalf("exit %d (stderr %q)", code, stderr)
			}
			info, err := os.Stat(filepath.Join(dir, ".cairn.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Errorf("mode %v, want 0600 under a 077 umask", got)
			}
		})
	}
}
