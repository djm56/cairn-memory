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

// noRoleNote is the line status prints under its directive count. The CLI's caller names no
// agent role, and nothing supplies one: identity reads no CAIRN_AGENT and no .cairn.toml key
// for it. So the server counts only directives not scoped to a role, and the line says so
// (14-W4). It goes away only when a role source is added to the CLI.
const noRoleNote = "agent-scoped directives not counted: no agent role is set"

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
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	var hc *http.Client
	if timeout > 0 {
		hc = &http.Client{Timeout: timeout}
	}
	svc, err := client.NewWithKey(url, key.Value, hc)
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

// loadKey returns the API key the calls send (DESIGN.md §2.1). A key that is in neither
// place is a core.Invalid whose text names both places, so every command that calls the
// server gives the same instruction. A key file that is open to other users is an error
// from the client, and it is returned as it is.
func loadKey() (client.Key, error) {
	key, err := client.LoadKey()
	if err != nil {
		return client.Key{}, err
	}
	if key.Source == "" {
		return client.Key{}, core.Invalid("no API key: %s", client.NoKeyMessage())
	}
	return key, nil
}

// reason is the text of err for a person: the message of a core.Error, without its
// kind, and the error's own text for any other failure. The message is server text, so
// it goes through plain (step 10 review W3, step 12 review W3): one printable line,
// capped at maxLineRunes, with no control or format character. Every print of reason's
// result is a print of server text or of an error that may carry it, so all of them
// get the same rule.
func reason(err error) string {
	var ce *core.Error
	if errors.As(err, &ce) {
		return plain(ce.Message)
	}
	return plain(err.Error())
}

// serverSource says where the server URL came from, for a person reading status or
// doctor. CAIRN_SERVER is read first, so it is named when it is set.
func serverSource() string {
	if os.Getenv(identity.EnvServer) != "" {
		return identity.EnvServer
	}
	return identity.FileName
}
