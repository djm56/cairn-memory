//go:build unix

package server

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// The lock bytes SQLite takes, as byte offsets in the file each lock is on. The probe reads
// them, so they are taken from SQLite's own sources and not assumed:
//
//   - The database file's locks lie in its lock-byte page: PENDING at 0x40000000, RESERVED at
//     0x40000001, and the SHARED range from 0x40000002 to 0x400001FF. The page is bytes
//     1073741824 to 1073742335 (SQLite file format, section 1.4, "The Lock-Byte Page":
//     https://sqlite.org/fileformat2.html). SQLite never reads or writes it, and every lock it
//     takes on the database file lies inside it.
//   - The -shm file's dead-man switch, DMS, is byte 128. UNIX_SHM_BASE is (22+SQLITE_SHM_NLOCK)*4,
//     which is 120 with SQLITE_SHM_NLOCK of 8, and DMS is UNIX_SHM_BASE+8 (os_unix.c, SQLite 3.54.1,
//     where the lock table is listed and unixLockSharedMemory takes it). Every connection that has a
//     WAL database open holds a shared lock on DMS, so an idle WAL server holds it too.
//     unixLockSharedMemory and _unixIsSharingShmNode test it with F_GETLK, as this probe does.
const (
	dbLockStart  int64 = 0x40000000
	dbLockLength int64 = 512
	shmDMSStart  int64 = 128
	shmDMSLength int64 = 1
)

// rawInUse reports whether another process holds a lock on the database at db, or on its -shm
// dead-man switch. Restore's raw path uses it, because no connection can be opened to ask a file
// that is not a readable database.
//
// The probe changes nothing. It opens each file read-only, asks the kernel with F_GETLK, which
// reports a conflicting lock and takes none, and closes the file. Nothing is written, created, or
// locked. It returns held when a lock is found, and an error when it cannot tell: a file it cannot
// open, or a call that fails. A -shm that does not exist holds no lock, because a connection with a
// WAL database open always creates one.
//
// Not every holder is seen. An idle connection in rollback-journal mode holds no lock at all, so a
// false result does not prove the database is free. The caller says so.
func rawInUse(db string) (bool, error) {
	held, err := lockHeld(db, dbLockStart, dbLockLength)
	if err != nil || held {
		return held, err
	}
	return lockHeld(db+"-shm", shmDMSStart, shmDMSLength)
}

// lockHeld reports whether a lock held by another process would conflict with an exclusive lock on
// [start, start+length) of path. F_GETLK reports only locks of other processes, so the locks this
// process holds are not seen. A file that does not exist holds no lock. The file is opened read-only
// and closed again. Closing a file releases every lock this process holds on it, so the probe must
// only run where this process holds no connection to that file. Restore's raw path ensures that.
func lockHeld(path string, start, length int64) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	lk := syscall.Flock_t{
		Type:   int16(syscall.F_WRLCK),
		Whence: int16(io.SeekStart), // SEEK_SET: the range is from the start of the file
		Start:  start,
		Len:    length,
	}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk); err != nil {
		return false, err
	}
	return lk.Type != int16(syscall.F_UNLCK), nil
}
