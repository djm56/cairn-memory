//go:build unix

package server

import "syscall"

// setOwnerOnlyUmask sets the process umask to 077 and returns a function that puts the previous
// umask back. The umask is process-wide, so the pair is not re-entrant: callers must not overlap,
// and Backup must never run from serve (see Backup).
func setOwnerOnlyUmask() (restore func()) {
	prev := syscall.Umask(0o077)
	return func() { syscall.Umask(prev) }
}
