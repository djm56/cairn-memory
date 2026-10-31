package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/djm56/cairn-memory/internal/server"
)

// defaultServeAddr is what cairn serve listens on when --addr is not given: loopback, on
// 8788, the port of the server example in DESIGN.md §2.1. Any other address needs TLS or
// --behind-proxy, and at least one API key (D20, DESIGN §8).
const defaultServeAddr = "127.0.0.1:8788"

// runServe is the sub-command `cairn serve` (DESIGN.md §5.2, §5.3; D20). It reads the
// flags and hands them to server.Run, which holds the bind rules, the store and the
// listener. This package names none of them, so the reach guard keeps the client side of
// the binary free of a database and a listener.
//
// Exit codes: 0 when the server stops on a signal, 1 when it fails to start or stop, 2
// for a usage error. `--help` prints to stdout and exits 0.
func runServe(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn serve", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	addr := fl.String("addr", defaultServeAddr, "host:port to listen on; loopback by default")
	db := fl.String("db", "", "the SQLite file the server keeps its data in (required)")
	certFile := fl.String("tls-cert", "", "PEM certificate file; give it with --tls-key")
	keyFile := fl.String("tls-key", "", "PEM private key file; give it with --tls-cert")
	behindProxy := fl.Bool("behind-proxy", false, "a proxy in front terminates TLS, so a non-loopback --addr needs no certificate")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fl.SetOutput(stdout)
			fmt.Fprintln(stdout, "Usage: cairn serve --db <file> [flags]")
			fl.PrintDefaults()
			return 0
		}
		fmt.Fprintf(stderr, "cairn serve: %v; run cairn serve --help for the flags\n", err)
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn serve: takes flags only, got %q\n", fl.Args())
		return 2
	}
	if *db == "" {
		fmt.Fprintln(stderr, "cairn serve: needs --db <file>: the SQLite file the server keeps its data in")
		return 2
	}
	if (*certFile == "") != (*keyFile == "") {
		fmt.Fprintln(stderr, "cairn serve: --tls-cert and --tls-key go together: give both, or neither")
		return 2
	}

	err := server.Run(context.Background(), server.Options{
		Addr:        *addr,
		DB:          *db,
		TLSCert:     *certFile,
		TLSKey:      *keyFile,
		BehindProxy: *behindProxy,
		Log:         stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
