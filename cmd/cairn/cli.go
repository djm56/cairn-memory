package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// workDir returns the directory a command runs from: the project it acts for is
// found by walking up from here (DESIGN.md §2.1). Tests replace it.
var workDir = os.Getwd

// cliClient is the client name the CLI gives the server in its caller, so the
// server can tell its calls from an MCP client's.
const cliClient = "cairn-cli"

// target is what a command needs to call the server for the project it runs in.
type target struct {
	project identity.Project
	url     string
	svc     core.Service
	// ctx carries the caller: the project, the client name and the agent role.
	// A read needs no agent, so none is set.
	ctx context.Context
}

// dial resolves the project and the server for a command run from wd, and returns a
// client for the server. timeout bounds each call; zero keeps the client default.
// No store is opened, and nothing listens: the server is the only place data lives
// (D11, D19). A missing project or server is the error that names the fix.
func dial(wd string, timeout time.Duration) (*target, error) {
	project, err := identity.Resolve(wd)
	if err != nil {
		return nil, err
	}
	url, err := identity.ResolveServer(wd)
	if err != nil {
		return nil, err
	}
	var hc *http.Client
	if timeout > 0 {
		hc = &http.Client{Timeout: timeout}
	}
	svc, err := client.New(url, hc)
	if err != nil {
		return nil, err
	}
	ctx := core.WithCaller(context.Background(), core.Caller{
		Project: project.Name,
		Client:  cliClient,
		Role:    core.RoleAgent,
	})
	return &target{project: project, url: url, svc: svc, ctx: ctx}, nil
}

// reason is the text of err for a person: the message of a core.Error, without its
// kind, and the error's own text for any other failure.
func reason(err error) string {
	var ce *core.Error
	if errors.As(err, &ce) {
		return ce.Message
	}
	return err.Error()
}

// serverSource says where the server URL came from, for a person reading status or
// doctor. CAIRN_SERVER is read first, so it is named when it is set.
func serverSource() string {
	if os.Getenv(identity.EnvServer) != "" {
		return identity.EnvServer
	}
	return identity.FileName
}
