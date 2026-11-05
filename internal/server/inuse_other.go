//go:build !unix

package server

import "errors"

// rawInUse cannot tell on this platform. The probe reads POSIX record locks with fcntl, and this
// build has none of them. It always returns an error, so Restore keeps its warning that the lock
// could not be checked.
func rawInUse(string) (bool, error) {
	return false, errors.New("the lock probe needs POSIX record locks, which this build does not use")
}
