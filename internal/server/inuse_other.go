//go:build !(darwin || linux || freebsd || openbsd)

package server

import "errors"

// rawInUse cannot tell on this platform. The probe reads POSIX record locks with fcntl, and is built
// only for the systems it was checked against (see inuse_unix.go). Here it always returns an error,
// so Restore keeps its warning that the lock could not be checked.
func rawInUse(string) (bool, error) {
	return false, errors.New("the lock probe is not built for this system")
}
