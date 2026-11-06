// Package hardlink says when a failed hard link means the filesystem cannot make one. It is the
// single list of those errnos: cmd/cairn (init) and internal/server (backup) both ask it, so the
// two cannot drift apart.
package hardlink

import (
	"errors"
	"syscall"
)

// Unsupported reports whether a failed link means the filesystem cannot make one: EPERM (FAT and
// exFAT on macOS), ENOTSUP and EOPNOTSUPP (one value on Linux, two on macOS), EXDEV, and ENOSYS (a
// mount that does not implement link). It is false for EEXIST, and for every other failure, which
// is returned rather than worked around.
func Unsupported(err error) bool {
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.EXDEV, syscall.ENOSYS} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
