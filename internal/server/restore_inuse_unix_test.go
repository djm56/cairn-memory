//go:build darwin || linux || freebsd || openbsd

package server

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// rawProbeBuilt says the lock probe of inuse_unix.go is built here, so a raw restore with no holder
// reports that the probe found no lock. inuse_other_test.go sets it false for the stub.
const rawProbeBuilt = true

// holdEnv selects how the helper process holds the database. Only startHolder sets it, and only on
// the helper's environment. The database path is in holdEnv+"_DB".
const holdEnv = "CAIRN_RESTORE_TEST_HOLD"

// TestRestoreHoldHelper is the helper process of the in-use tests, not a test of its own. With
// holdEnv set it holds the database the way a running server would, prints "ready", and waits until
// its standard input closes. Without holdEnv it is skipped.
func TestRestoreHoldHelper(t *testing.T) {
	mode := os.Getenv(holdEnv)
	if mode == "" {
		t.Skip("the helper process of the in-use tests")
	}
	db := os.Getenv(holdEnv + "_DB")
	switch mode {
	case "wal":
		// An open store is a WAL database, and each of its connections holds the dead-man switch
		// of the -shm file.
		st, err := store.Open(db)
		if err != nil {
			t.Fatalf("hold %s as a WAL database: %v", db, err)
		}
		defer st.Close()
	case "read":
		// A read transaction in rollback-journal mode holds a SHARED lock on the database file.
		conn, err := sql.Open("sqlite", db+"?_busy_timeout=5000")
		if err != nil {
			t.Fatalf("open %s: %v", db, err)
		}
		conn.SetMaxOpenConns(1)
		defer conn.Close()
		if _, err := conn.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
			t.Fatalf("switch %s to rollback-journal mode: %v", db, err)
		}
		if _, err := conn.Exec(`BEGIN`); err != nil {
			t.Fatalf("begin on %s: %v", db, err)
		}
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
			t.Fatalf("read %s: %v", db, err)
		}
	case "dms":
		// The dead-man switch alone: a shared lock on byte 128 of the -shm, and nothing on the
		// database file. The offset is written out here and not taken from the probe's constant, so
		// a wrong constant in the probe fails this test. Byte 128 is SQLite's DMS offset (see
		// inuse_unix.go).
		f, err := os.OpenFile(db+"-shm", os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatalf("create the -shm of %s: %v", db, err)
		}
		defer f.Close()
		lk := syscall.Flock_t{Type: int16(syscall.F_RDLCK), Whence: int16(io.SeekStart), Start: 128, Len: 1}
		if err := syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lk); err != nil {
			t.Fatalf("take a shared lock on the dead-man switch of %s: %v", db, err)
		}
	default:
		t.Fatalf("unknown hold mode %q", mode)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// startHolder starts a helper process that holds the database at db in mode, and returns once the
// helper says it holds it, or fails the test if it has not said so within holderReadyTimeout. The helper runs until the test ends, when its standard input closes.
// The holder must be another process: POSIX locks belong to the process that takes them, so a lock
// held by this process is not one the probe can see.
func startHolder(t *testing.T, mode, db string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRestoreHoldHelper$")
	cmd.Env = append(os.Environ(), holdEnv+"="+mode, holdEnv+"_DB="+db)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	// The handshake has a deadline, so a helper that wedges before it says "ready" fails the test
	// here instead of hanging it until the go test timeout. The helper is killed, which ends the read.
	type handshake struct {
		line string
		err  error
	}
	got := make(chan handshake, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		got <- handshake{line, err}
	}()
	select {
	case h := <-got:
		if h.err != nil || strings.TrimSpace(h.line) != "ready" {
			t.Fatalf("the helper did not hold %s as %s: read %q, %v", db, mode, h.line, h.err)
		}
	case <-time.After(holderReadyTimeout):
		_ = cmd.Process.Kill()
		t.Fatalf("the helper did not say it holds %s as %s within %v", db, mode, holderReadyTimeout)
	}
}

// holderReadyTimeout is how long startHolder waits for the helper to say it holds the database. A
// helper starts and opens one database, so this is far longer than a healthy one needs.
const holderReadyTimeout = 30 * time.Second

// damageHeader overwrites the first 16 bytes of the file at path, so that it is no longer a
// readable SQLite database. The file keeps its inode, so a helper that holds it still holds the
// file that is at the path.
func damageHeader(t *testing.T, path string) {
	t.Helper()
	data := readBytes(t, path)
	copy(data[:16], []byte("XXXXXXXXXXXXXXXX"))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertRefusedInUse runs a restore of the raw database at target and fails unless it is refused as
// in use before the prompt, with nothing moved, nothing left staged, and every byte as it was.
func assertRefusedInUse(t *testing.T, dir, target, backup string) {
	t.Helper()
	before := listDir(t, dir)
	bytesBefore := readBytes(t, target)

	opts, out := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "is in use") || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("Restore of a raw database held by another process: err = %v, want the in-use refusal; output %q", err, out.String())
	}
	if strings.Contains(out.String(), "To confirm") {
		t.Errorf("the refusal came after the prompt; the in-use check must come first: output %q", out.String())
	}
	if got := listDir(t, dir); !slices.Equal(got, before) {
		t.Errorf("a refused restore changed the directory: %v, was %v", got, before)
	}
	if got := readBytes(t, target); string(got) != string(bytesBefore) {
		t.Errorf("a refused restore changed the bytes of the database")
	}
}

// TestRestoreRefusesARawDatabaseHeldByAnIdleServer is the case a server leaves behind. It holds a
// WAL database and is idle. A real idle connection holds a shared lock on the database file and the
// dead-man switch of the -shm, and either is enough for the probe. The header is then damaged, so
// the restore reads the file as raw, and it must refuse.
func TestRestoreRefusesARawDatabaseHeldByAnIdleServer(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	startHolder(t, "wal", target)
	damageHeader(t, target)
	assertRefusedInUse(t, dir, target, backup)
}

// TestRestoreRefusesARawDatabaseWithAReaderInATransaction is the case of the database file's own
// lock bytes. A reader holds a read transaction in rollback-journal mode, so it holds a SHARED lock
// on the database file and no -shm. The probe must find the SHARED lock and refuse.
func TestRestoreRefusesARawDatabaseWithAReaderInATransaction(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	startHolder(t, "read", target)
	damageHeader(t, target)
	assertRefusedInUse(t, dir, target, backup)
}

// TestRestoreRefusesARawDatabaseHeldOnlyByItsDeadManSwitch isolates the -shm probe. The holder
// takes the dead-man switch and nothing on the database file, so only byte 128 of the -shm can
// find it. The probe must find it and refuse. The offset in the test is not the probe's constant,
// so a wrong constant fails here.
func TestRestoreRefusesARawDatabaseHeldOnlyByItsDeadManSwitch(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	startHolder(t, "dms", target)
	damageHeader(t, target)
	assertRefusedInUse(t, dir, target, backup)
}

// TestRawInUseChangesNothing checks the probe alone, with no holder. It must report no lock, return
// no error, and leave every file with the same name, bytes and modification time. Opening a file for
// the probe must not write to it. With the database alone, no -wal or -shm, the probe must create
// neither: a creating open would make the -shm it asks about.
func TestRawInUseChangesNothing(t *testing.T) {
	cases := map[string][]string{
		"with its -wal and -shm": {"cairn.db", "cairn.db-wal", "cairn.db-shm"},
		"the database alone":     {"cairn.db"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db := filepath.Join(dir, "cairn.db")
			for _, f := range files {
				content := "a " + f + " of nothing"
				if f == "cairn.db" {
					content = strings.Repeat("this is not a database\n", 200)
				}
				if err := os.WriteFile(filepath.Join(dir, f), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			names := listDir(t, dir)
			bytesBefore := map[string][]byte{}
			timesBefore := map[string]time.Time{}
			for _, f := range files {
				bytesBefore[f] = readBytes(t, filepath.Join(dir, f))
				fi, err := os.Stat(filepath.Join(dir, f))
				if err != nil {
					t.Fatal(err)
				}
				timesBefore[f] = fi.ModTime()
			}

			held, err := rawInUse(db)
			if held || err != nil {
				t.Fatalf("rawInUse with no holder: held = %v, err = %v; want false and nil", held, err)
			}
			// The listing is the check that nothing was created: a -wal or -shm the probe made would
			// show here as a new name.
			if got := listDir(t, dir); !slices.Equal(got, names) {
				t.Errorf("the probe changed the directory: %v, was %v", got, names)
			}
			for f, want := range bytesBefore {
				path := filepath.Join(dir, f)
				if got := readBytes(t, path); string(got) != string(want) {
					t.Errorf("the probe changed the bytes of %s", f)
				}
				fi, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !fi.ModTime().Equal(timesBefore[f]) {
					t.Errorf("the probe changed the modification time of %s", f)
				}
			}
		})
	}
}

// TestRestoreSaysTheLockCouldNotBeCheckedWhenTheProbeCannotOpenIt checks the branch where the probe
// cannot tell. The -shm is not readable, so the probe cannot ask about it. The restore must not
// refuse, since nothing is known to hold the file, and the prompt must keep the warning that the
// lock could not be checked. The check needs a process that is not root, since root opens a file
// whatever its mode.
func TestRestoreSaysTheLockCouldNotBeCheckedWhenTheProbeCannotOpenIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root process opens a file whatever its mode, so the probe cannot be made to fail")
	}
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	if err := os.WriteFile(target, []byte(strings.Repeat("this is not a database\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+"-shm", []byte("a shm the probe cannot open"), 0o000); err != nil {
		t.Fatal(err)
	}

	opts, out := restoreRun(target, backup, target+"\n", true)
	if err := Restore(context.Background(), opts); err != nil {
		t.Fatalf("Restore of a raw target whose -shm the probe cannot open: %v", err)
	}
	if !strings.Contains(out.String(), "The lock could not be checked") {
		t.Errorf("the prompt %q does not say the lock could not be checked", out.String())
	}
}
