package server

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestSyncPathToleratesOnlyADirectorySyncRefusal checks 14b-S5. A directory whose sync the file
// system refuses with EINVAL or ENOTSUP is treated as synced. Any other error from a directory
// sync is still an error, and so is a refused sync of a file.
func TestSyncPathToleratesOnlyADirectorySyncRefusal(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := syncFile
	t.Cleanup(func() { syncFile = prev })

	cases := []struct {
		name    string
		path    string
		injects error
		wantErr bool
	}{
		{"directory EINVAL is tolerated", dir, syscall.EINVAL, false},
		{"directory ENOTSUP is tolerated", dir, syscall.ENOTSUP, false},
		{"directory EIO is an error", dir, syscall.EIO, true},
		{"file EINVAL is an error", file, syscall.EINVAL, true},
		{"file ENOTSUP is an error", file, syscall.ENOTSUP, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			syncFile = func(*os.File) error { return c.injects }
			err := syncPath(c.path)
			if c.wantErr && !errors.Is(err, c.injects) {
				t.Errorf("syncPath = %v, want the %v error", err, c.injects)
			}
			if !c.wantErr && err != nil {
				t.Errorf("syncPath = %v, want nil", err)
			}
		})
	}
}
