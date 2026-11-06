//go:build !(darwin || linux || freebsd || openbsd)

package server

// rawProbeBuilt says the lock probe is not built here: rawInUse is the stub of inuse_other.go, which
// always errors, so a raw restore reports that the lock could not be checked.
const rawProbeBuilt = false
