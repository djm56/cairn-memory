package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// doctorTimeout bounds the one reachability call of cairn doctor, so a server that
// does not answer ends the check, not the command.
const doctorTimeout = 5 * time.Second

// check is one line of cairn doctor: what was checked, its status and the detail.
type check struct {
	name   string
	status string // "ok", "fail", or "skip" when an earlier check left nothing to check
	detail string
}

// runDoctor is the sub-command `cairn doctor` (DESIGN.md §5.2): the binary, the
// project, the server and whether the server answers as a Cairn server. It is
// read-only. The reachability call carries no project, so the server refuses it
// before any read or write, and it creates nothing (CONTRACT §9 item 38).
//
// Every check runs, even after one fails, so the output shows every fault. Exit
// codes: 0 when no check failed, 1 when one did, 2 for a usage error.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "cairn doctor: takes no arguments yet, got %q\n", args)
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
	if url, err := identity.ResolveServer(wd); err != nil {
		checks = append(checks, check{name: "server", status: "fail", detail: reason(err)})
		checks = append(checks, check{name: "reach", status: "skip", detail: "no server to reach"})
	} else {
		checks = append(checks, check{name: "server", status: "ok", detail: fmt.Sprintf("%s (from %s)", client.DisplayURL(url), serverSource())})
		checks = append(checks, reachCheck(url))
	}

	failed := false
	for _, c := range checks {
		fmt.Fprintf(stdout, "%-8s %-5s %s\n", c.name, c.status, c.detail)
		failed = failed || c.status == "fail"
	}
	if failed {
		return 1
	}
	return 0
}

// reachCheck asks the server a question that needs no project. A Cairn server
// answers invalid, "no project", and that answer shows it speaks the wire form.
// Any success, or any failure that is not a core error, is a fault: a server that
// answers a call with no project is not the one this is meant to be.
func reachCheck(url string) check {
	svc, err := client.New(url, &http.Client{Timeout: doctorTimeout})
	if err != nil {
		return check{name: "reach", status: "fail", detail: reason(err)}
	}
	_, err = svc.TaskCurrent(context.Background())
	var ce *core.Error
	switch {
	case err == nil:
		return check{name: "reach", status: "fail", detail: "the server answered a call with no project, which a Cairn server refuses"}
	case errors.As(err, &ce) && ce.Kind == core.KindInvalid:
		return check{name: "reach", status: "ok", detail: "the server answers as a Cairn server; no project was sent, so nothing was read or written"}
	default:
		return check{name: "reach", status: "fail", detail: reason(err)}
	}
}
