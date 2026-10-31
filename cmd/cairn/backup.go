package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/server"
)

// backupUsage is the help for cairn backup. It goes to stdout when asked for, and to stderr after
// a usage error.
const backupUsage = "Usage: cairn backup --db <file> <out>\n\n" +
	"Writes a consistent copy of the server's database to <out>, a new file with mode 0600.\n" +
	"The server may be running. An existing <out> is refused, never written over.\n"

// runBackup is the sub-command `cairn backup` (DESIGN.md §5.2, M2 #74): it names the database
// and the output file, and hands them to server.Backup, which opens the database and writes the
// copy. This package names no store (the reach guard). Exit codes: 0 when the copy is written,
// 1 when it is refused or fails, 2 for a usage error. `--help` prints to stdout and exits 0.
func runBackup(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn backup", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	db := fl.String("db", "", "the SQLite file the server keeps its data in (required)")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, backupUsage)
			return 0
		}
		fmt.Fprintf(stderr, "cairn backup: %v; run cairn backup --help for the flags\n", err)
		return 2
	}
	if *db == "" {
		fmt.Fprintf(stderr, "cairn backup: needs --db <file>: the database to copy\n")
		return 2
	}
	if fl.NArg() != 1 {
		fmt.Fprintf(stderr, "cairn backup: needs one output file, got %d: cairn backup --db <file> <out>\n", fl.NArg())
		return 2
	}
	out := fl.Arg(0)
	size, err := server.Backup(context.Background(), *db, out)
	if err != nil {
		fmt.Fprintf(stderr, "cairn backup: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "backed up %s to %s (%d bytes)\n", *db, out, size)
	return 0
}
