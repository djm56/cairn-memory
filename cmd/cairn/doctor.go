package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// doctorTimeout bounds the one reachability call of cairn doctor, so a server that
// does not answer ends the check, not the command. It is a variable so a test can wait
// on a server that never answers for less than the real five seconds.
var doctorTimeout = 5 * time.Second

// check is one line of cairn doctor: what was checked, its status and the detail.
type check struct {
	name   string
	status string // "ok"; "warn" for a finding that does not fail the check; "fail"; or "skip" when an earlier check left nothing to check
	detail string
}

// runDoctor is the sub-command `cairn doctor` (DESIGN.md §5.2): the binary, the
// project, the server and whether the server answers as a Cairn server. It is
// read-only. The reachability call carries no project, so the server refuses it
// before any read or write, and it creates nothing (CONTRACT §9 item 38).
//
// With --agent claude-code it adds a section after those checks: the cairn hooks
// found in the Claude Code settings files, and a dry run of each hook (doctor_agent.go).
// That section writes no file. Its dry run is not read-only on the server: the first
// read of a project the server has not seen creates the project's row, as the hooks do.
//
// Every check runs, even after one fails, so the output shows every fault. Exit
// codes: 0 when no check failed, 1 when one did, 2 for a usage error. A usage error
// is found before any check runs, so it prints nothing on stdout.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("cairn doctor", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	agent := fl.String("agent", "", "also check a client's hooks: claude-code")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeDoctorHelp(stdout, fl)
			return 0
		}
		fmt.Fprintf(stderr, "cairn doctor: %v; run cairn doctor --help for the flags\n", err)
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "cairn doctor: takes flags only, got %q\n", fl.Args())
		return 2
	}
	switch *agent {
	case "", "claude-code":
	case "opencode", "pi":
		fmt.Fprintf(stderr, "cairn doctor: --agent %s is not built yet: use --agent claude-code\n", *agent)
		return 2
	default:
		fmt.Fprintf(stderr, "cairn doctor: unknown agent %q: use --agent claude-code\n", *agent)
		return 2
	}

	wd, err := workDir()
	if err != nil {
		fmt.Fprintf(stderr, "cairn doctor: the working directory: %v\n", err)
		return 1
	}

	checks := []check{{name: "binary", status: "ok", detail: "cairn " + version}}
	if project, err := identity.Resolve(wd); err != nil {
		checks = append(checks, check{name: "project", status: "fail", detail: reason(err)})
	} else {
		checks = append(checks, check{name: "project", status: "ok", detail: fmt.Sprintf("%s (from %s)", project.Name, projectSource(project))})
	}
	url, serverErr := identity.ResolveServer(wd)
	if serverErr != nil {
		checks = append(checks, check{name: "server", status: "fail", detail: reason(serverErr)})
	} else {
		checks = append(checks, check{name: "server", status: "ok", detail: fmt.Sprintf("%s (from %s)", client.DisplayURL(url), serverSource())})
	}
	key, keyErr := client.LoadKey()
	keyChk := keyCheck(key, keyErr)
	checks = append(checks, keyChk)
	switch {
	case serverErr != nil:
		checks = append(checks, check{name: "reach", status: "skip", detail: "no server to reach"})
	case keyChk.status != "ok":
		checks = append(checks, check{name: "reach", status: "skip", detail: "no API key to send"})
	default:
		checks = append(checks, reachCheck(url, key.Value))
	}

	if serverErr == nil && keyChk.status == "ok" && client.SendsKeyInClear(url) {
		checks = append(checks, check{name: "transport", status: "warn", detail: fmt.Sprintf(
			"the API key goes to %s over http, in clear text, to a host that is not loopback: give the server an https address",
			client.DisplayURL(url))})
	}

	for _, c := range checks {
		fmt.Fprintf(stdout, "%-8s %-5s %s\n", c.name, c.status, c.detail)
	}
	failed := anyFailed(checks)
	if *agent == "claude-code" {
		agentChecks := append(claudeCodeChecks(wd), dryRunChecks()...)
		fmt.Fprintln(stdout, "hooks for claude-code")
		for _, c := range agentChecks {
			fmt.Fprintf(stdout, "%-12s %-5s %s\n", c.name, c.status, c.detail)
		}
		failed = failed || anyFailed(agentChecks)
	}
	if failed {
		return 1
	}
	return 0
}

// writeDoctorHelp prints the usage of cairn doctor, and its flags, to w.
func writeDoctorHelp(w io.Writer, fl *flag.FlagSet) {
	fmt.Fprintln(w, "Usage: cairn doctor [--agent claude-code]")
	fmt.Fprintln(w, "Checks the binary, the project file, the server and whether it answers as a Cairn server. Without --agent it changes nothing.")
	fmt.Fprintln(w, "With --agent claude-code it also lists the cairn hooks in the Claude Code settings files, and runs each hook once against the server. No file is written.")
	fmt.Fprintln(w, "Each hook reads the active task. The first read of a project the server has not seen creates the project's row, as the hooks do.")
	fmt.Fprintln(w, "Agents: claude-code. opencode and pi are not built yet.")
	fl.SetOutput(w)
	fl.PrintDefaults()
}

// anyFailed reports whether a check failed. Only "fail" fails the command: "warn" is printed
// and does not change the exit code.
func anyFailed(checks []check) bool {
	for _, c := range checks {
		if c.status == "fail" {
			return true
		}
	}
	return false
}

// keyCheck is the key line: where the key was found, and never the key. A key found in either
// place is ok. A key that is missing fails, and so does a key file the client refuses, with
// the reason it gives.
func keyCheck(key client.Key, err error) check {
	if err != nil {
		return check{name: "key", status: "fail", detail: reason(err)}
	}
	if key.Source == "" {
		return check{name: "key", status: "fail", detail: "missing: " + client.NoKeyMessage()}
	}
	return check{name: "key", status: "ok", detail: "found (" + string(key.Source) + ")"}
}

// reachCheck asks the server a question that needs no project, with the key. A Cairn server
// answers invalid, "no project", and that answer shows it speaks the wire form and accepted
// the key. Any success, or any failure that is not a core error, is a fault: a server that
// answers a call with no project is not the one this is meant to be. A refused key is a
// fault, and its message says so.
func reachCheck(url, key string) check {
	svc, err := client.NewWithKey(url, key, &http.Client{Timeout: doctorTimeout})
	if err != nil {
		return check{name: "reach", status: "fail", detail: reason(err)}
	}
	_, err = svc.TaskCurrent(context.Background())
	var ce *core.Error
	switch {
	case err == nil:
		return check{name: "reach", status: "fail", detail: "the server answered a call with no project, which a Cairn server refuses"}
	case errors.As(err, &ce) && ce.Kind == core.KindInvalid:
		return check{name: "reach", status: "ok", detail: "the server answers as a Cairn server and accepted the key; no project was sent, so nothing was read or written"}
	default:
		return check{name: "reach", status: "fail", detail: reason(err)}
	}
}
