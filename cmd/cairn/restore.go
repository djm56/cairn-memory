package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/djm56/cairn-memory/internal/server"
)

// restoreInput is where the confirmation is typed: stdin. A test sets it to a reader it controls.
var restoreInput io.Reader = os.Stdin

// stdinIsTerminal reports whether stdin is a terminal. A restore is confirmed by a person at one,
// so a restore with stdin piped or redirected is refused (M2 #74). A test replaces it.
var stdinIsTerminal = func() bool {
	fd := os.Stdin.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// restoreUsage is the help for cairn restore. It goes to stdout when asked for, and to stderr
// after a usage error.
const restoreUsage = "Usage: cairn restore --db <file> <backup>\n\n" +
	"Replaces the server's database at <file> with the backup. The backup is checked first. The\n" +
	"database there now is moved, with its -wal and -shm, to <file>.pre-restore-<UTC time>, and\n" +
	"is never deleted. You confirm by typing <file> at a terminal. There is no flag to skip it.\n"

// runRestore is the sub-command `cairn restore` (DESIGN.md §5.2, M2 #74): it names the database
// and the backup, and hands them to server.Restore, which checks the backup, takes the lock,
// asks for the confirmation and moves the files. This package names no store (the reach guard).
// Exit codes: 0 when the database is restored, 1 when a step refuses or fails, 2 for a usage
// error. `--help` prints to stdout and exits 0.
func runRestore(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn restore", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	db := fl.String("db", "", "the SQLite file to replace (required)")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, restoreUsage)
			return 0
		}
		fmt.Fprintf(stderr, "cairn restore: %v; run cairn restore --help for the flags\n", err)
		return 2
	}
	if *db == "" {
		fmt.Fprintf(stderr, "cairn restore: needs --db <file>: the database to replace\n")
		return 2
	}
	if fl.NArg() != 1 {
		fmt.Fprintf(stderr, "cairn restore: needs one backup file, got %d: cairn restore --db <file> <backup>\n", fl.NArg())
		return 2
	}
	err := server.Restore(context.Background(), server.RestoreOptions{
		DB:       *db,
		Backup:   fl.Arg(0),
		Terminal: stdinIsTerminal(),
		Input:    restoreInput,
		Out:      stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "cairn restore: %v\n", err)
		return 1
	}
	return 0
}
