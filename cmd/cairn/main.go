// Command cairn is the single Cairn binary: MCP server, HTTP server, hook
// helper and admin tool (docs/DESIGN.md §5.2). Each job is a sub-command.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// version is set at build time with -ldflags "-X main.version=..." from
// git describe (see the Makefile). A plain go build reports "dev".
var version = "dev"

// command is one sub-command. run receives the arguments after the
// sub-command name and returns the process exit code.
type command struct {
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is filled in init so that help can list it without an
// initialisation cycle. Sub-commands from DESIGN.md §5.2 are added here as
// the phases in docs/PLAN.md build them.
var commands map[string]command

func init() {
	commands = map[string]command{
		"version": {summary: "Print the cairn version", run: runVersion},
		"help":    {summary: "List the sub-commands", run: runHelp},
		"mcp":     {summary: "Serve the MCP tools over stdio, through the Cairn server", run: runMCP},
		"init":    {summary: "Write .cairn.toml: the project name and the server", run: runInit},
		"export":  {summary: "Print a task as Markdown: export task [id]", run: runExport},
		"status":  {summary: "Show the server, the project and the active task", run: runStatus},
		"doctor":  {summary: "Check the project file, the server and the connection; changes nothing", run: runDoctor},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "-h", "-help", "--help":
		usage(stdout)
		return 0
	case "-v", "-version", "--version":
		return runVersion(nil, stdout, stderr)
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "cairn: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	return cmd.run(args[1:], stdout, stderr)
}

func runVersion(_ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "cairn %s\n", version)
	return 0
}

func runHelp(_ []string, stdout, _ io.Writer) int {
	usage(stdout)
	return 0
}

func usage(w io.Writer) {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(w, "Usage: cairn <command> [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, name := range names {
		fmt.Fprintf(w, "  %-10s %s\n", name, commands[name].summary)
	}
}
