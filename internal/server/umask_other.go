//go:build !unix

package server

// setOwnerOnlyUmask does nothing on a platform with no umask to set. The copy is then protected
// only by the 0700 staging directory and by the chmod that follows the copy. The restore function
// it returns is a no-op, so callers are the same on every platform.
func setOwnerOnlyUmask() (restore func()) {
	return func() {}
}
