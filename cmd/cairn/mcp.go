package main

import (
	"context"
	"fmt"
	"io"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/identity"
	"github.com/djm56/cairn-memory/internal/mcp"
)

// runMCP is the sub-command `cairn mcp`: the MCP tools over stdio (DESIGN.md §5.2).
// The protocol is on the process's own stdin and stdout.
func runMCP(args []string, stdout, stderr io.Writer) int {
	return serveMCP(args, os.Stdin, stdout, stderr)
}

// serveMCP serves the MCP tools over stdin and stdout until the client goes away.
// It is the server-only adapter (D11, DESIGN §5.3): it never opens a database. Each
// call goes to the server named by CAIRN_SERVER or the .cairn.toml server key, and
// the server is required, so there is no local fallback. The call carries the API key
// from CAIRN_API_KEY or client.env, and a process with no key refuses to start (DESIGN §2.1). It listens on nothing (D19):
// the client dials the server, and this process only answers its own client.
//
// The project is resolved once, at start. When it does not resolve, the server still
// starts, and each call is refused with the reason, as DESIGN §2.1 asks.
//
// The return value is the exit code. Protocol traffic is the only thing written to
// stdout; every other message goes to stderr.
func serveMCP(args []string, stdin io.ReadCloser, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "cairn mcp: takes no arguments, got %q\n", args)
		return 2
	}
	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn mcp: the working directory: %v\n", err)
		return 1
	}

	url, err := identity.ResolveServer(wd)
	if err != nil {
		fmt.Fprintf(stderr, "cairn mcp: %v\n", err)
		return 1
	}
	key, err := loadKey()
	if err != nil {
		fmt.Fprintf(stderr, "cairn mcp: %v\n", err)
		return 1
	}
	svc, err := client.NewWithKey(url, key.Value, nil)
	if err != nil {
		fmt.Fprintf(stderr, "cairn mcp: %v\n", err)
		return 1
	}

	project := mcp.Project{}
	if p, err := identity.Resolve(wd); err != nil {
		project.Err = err
	} else {
		project.Name = p.Name
	}

	srv := mcp.New(svc, project, version)
	transport := &mcpsdk.IOTransport{Reader: stdin, Writer: nopWriteCloser{stdout}}
	if err := srv.Run(context.Background(), transport); err != nil {
		fmt.Fprintf(stderr, "cairn mcp: %v\n", err)
		return 1
	}
	return 0
}

// nopWriteCloser gives a writer the Close that IOTransport asks for. Closing the
// protocol output is not the adapter's to do, since the process owns stdout.
type nopWriteCloser struct {
	io.Writer
}

// Close does nothing. The writer belongs to the caller.
func (nopWriteCloser) Close() error { return nil }
