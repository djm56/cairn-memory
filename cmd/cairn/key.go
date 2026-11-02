package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/server"
)

// keyUsage is the help for cairn key. It goes to stdout when asked for, and to stderr after
// a usage error.
const keyUsage = `Usage: cairn key <action> --db <file> [flags]

  cairn key add    --db <file> --name <name> --scope agent|operator|viewer
  cairn key list   --db <file>
  cairn key revoke --db <file> --name <name>

The key add prints is shown once, on stdout, and cannot be shown again: store it when it
appears. The database must be the one cairn serve keeps its data in.
`

// runKey is the sub-command `cairn key` (DESIGN.md §5.2, §8): it makes, lists and revokes the
// API keys of a server's database. It parses the flags and hands them to the server package,
// which opens the database, so this package names no store and no listener (the reach
// guard). Exit codes: 0 when the action is done, 1 when the database or the key refuses it,
// 2 for a usage error. `--help` prints to stdout and exits 0.
func runKey(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, keyUsage)
		return 2
	}
	switch args[0] {
	case "add":
		return runKeyAdd(args[1:], stdout, stderr)
	case "list":
		return runKeyList(args[1:], stdout, stderr)
	case "revoke":
		return runKeyRevoke(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, keyUsage)
		return 0
	}
	fmt.Fprintf(stderr, "cairn key: unknown action %q: use add, list or revoke; run cairn key --help\n", args[0])
	return 2
}

// keyFlag is one flag of a key action: its name, and where its value goes. Every key flag is
// required, so an empty value is a usage error.
type keyFlag struct {
	name  string
	value *string
}

// keyFlags parses the flags of one key action into the named values, and checks that each
// is given. It returns the exit code to stop with, or -1 to go on. A help request prints the
// usage to stdout. The flags are checked in the order given, so the message is the same on
// every run.
func keyFlags(action string, args []string, stdout, stderr io.Writer, flags []keyFlag) int {
	fl := flag.NewFlagSet("cairn key "+action, flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	for _, f := range flags {
		fl.StringVar(f.value, f.name, "", "")
	}
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, keyUsage)
			return 0
		}
		fmt.Fprintf(stderr, "cairn key %s: %v; run cairn key --help for the flags\n", action, err)
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn key %s: takes flags only, got %q\n", action, fl.Args())
		return 2
	}
	for _, f := range flags {
		if *f.value == "" {
			fmt.Fprintf(stderr, "cairn key %s: needs --%s\n", action, f.name)
			return 2
		}
	}
	return -1
}

// runKeyAdd is `cairn key add`: it makes a key, prints it once on stdout, and warns on
// stderr that it is shown once. Nothing about the key is logged.
//
// When the key cannot be written to stdout, the key was made and no one has seen it, and it
// cannot be shown again. add revokes it, so no live key exists that nobody has, and exits 1
// saying so. The name stays taken, since a name is not reused, so a retry needs a new name.
// If the revoke fails too, the message names the key so the operator can revoke it by hand.
// A database that add creates is named on stderr, so a mistyped path that makes one is
// visible (8-S4).
func runKeyAdd(args []string, stdout, stderr io.Writer) int {
	db, name, scope := new(string), new(string), new(string)
	flags := []keyFlag{{"db", db}, {"name", name}, {"scope", scope}}
	if code := keyFlags("add", args, stdout, stderr, flags); code >= 0 {
		return code
	}
	if !auth.ValidScope(auth.Scope(*scope)) {
		fmt.Fprintf(stderr, "cairn key add: --scope must be agent, operator or viewer\n")
		return 2
	}
	_, statErr := os.Stat(*db)
	created := errors.Is(statErr, fs.ErrNotExist)
	key, err := server.AddKey(context.Background(), *db, *name, auth.Scope(*scope))
	if err != nil {
		fmt.Fprintf(stderr, "cairn key add: %v\n", err)
		return 1
	}
	// A write to a closed stdout pipe makes the Go runtime send SIGPIPE, which ends the
	// process before the revoke below can run. Ignoring it for this one write makes the write
	// return EPIPE instead, so the key is revoked (10-W3). The ignore is reset straight after.
	signal.Ignore(syscall.SIGPIPE)
	_, werr := fmt.Fprintln(stdout, key)
	signal.Reset(syscall.SIGPIPE)
	if werr != nil {
		if rerr := server.RevokeKey(context.Background(), *db, *name); rerr != nil {
			fmt.Fprintf(stderr, "cairn key add: the key named %s was made, but it could not be shown (%v) and could not be revoked (%v): revoke it with cairn key revoke --db %s --name %s\n",
				*name, werr, rerr, *db, *name)
			return 1
		}
		fmt.Fprintf(stderr, "cairn key add: the key named %s was made, but it could not be shown (%v), so it was revoked: add again with a new name\n", *name, werr)
		return 1
	}
	if created {
		fmt.Fprintf(stderr, "cairn key add: created %s\n", *db)
	}
	fmt.Fprintln(stderr, "cairn key add: the key is shown once above: store it now, it cannot be shown again")
	return 0
}

// runKeyList is `cairn key list`: each key's name, scope and times. Neither the hash nor the
// key is printed, since the list does not hold them.
func runKeyList(args []string, stdout, stderr io.Writer) int {
	db := new(string)
	if code := keyFlags("list", args, stdout, stderr, []keyFlag{{"db", db}}); code >= 0 {
		return code
	}
	keys, err := server.ListKeys(context.Background(), *db)
	if err != nil {
		fmt.Fprintf(stderr, "cairn key list: %v\n", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSCOPE\tCREATED\tLAST USED\tREVOKED")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k.Name, k.Scope, keyTime(&k.CreatedAt), keyTime(k.LastUsedAt), keyTime(k.RevokedAt))
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "cairn key list: write the list: %v\n", err)
		return 1
	}
	if len(keys) == 0 {
		fmt.Fprintln(stderr, "cairn key list: no keys: make one with cairn key add")
	}
	return 0
}

// runKeyRevoke is `cairn key revoke`: it revokes the active key named --name. A second revoke
// of the same name, or a name with no key, is exit 1 with the reason.
func runKeyRevoke(args []string, stdout, stderr io.Writer) int {
	db, name := new(string), new(string)
	flags := []keyFlag{{"db", db}, {"name", name}}
	if code := keyFlags("revoke", args, stdout, stderr, flags); code >= 0 {
		return code
	}
	if err := server.RevokeKey(context.Background(), *db, *name); err != nil {
		fmt.Fprintf(stderr, "cairn key revoke: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "revoked %s: the key stops working at its next call\n", *name)
	return 0
}

// keyTime formats a key's time for the list, in UTC, or "-" when there is none.
func keyTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
