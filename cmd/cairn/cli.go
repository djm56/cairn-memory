package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/cli"
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

// noRoleNote is the line status prints under its directive count, and the session-start hook
// prints in its own words, when no agent role is set. With no role the server counts only the
// directives not scoped to one, and the line says so. A role comes from CAIRN_AGENT (roleFromEnv).
const noRoleNote = "agent-scoped directives not counted: no agent role is set"

// refusedRoleNote is the line status prints when CAIRN_AGENT is set but the wire form refuses it.
// It names the variable and the reason, so a person does not read it as the variable being unset.
const refusedRoleNote = "agent-scoped directives not counted: CAIRN_AGENT is set but refused (a control character or a space at either end)"

// target is what a command needs to call the server for the project it runs in.
type target struct {
	project identity.Project
	url     string
	svc     core.Service
	// ctx carries the caller: the project and the client name. A read names its agent role on
	// the call, from role (currentFor), not here.
	ctx context.Context
	// role is the agent role that status and the session-start hook count agent-scoped
	// directives for: CAIRN_AGENT, when it is set and valid. Empty when there is none.
	role string
	// roleRefused is true when CAIRN_AGENT is set but refused, so role is empty for a reason
	// other than the variable being unset or blank.
	roleRefused bool
}

// roleFromEnv returns the agent role for the agent-scoped directive counts: CAIRN_AGENT, when it
// is set to a name the wire form carries. A value that is unset or blank is no role. A value that
// has a control character or a space at either end is refused: the role is empty and refused is
// true, so the block can say the variable was refused rather than unset. The name is not checked
// against the agents that exist: a role names directives, and a role with none lists none.
func roleFromEnv() (role string, refused bool) {
	role = os.Getenv(identity.EnvAgent)
	if strings.TrimSpace(role) == "" {
		return "", false
	}
	if core.CheckCaller(core.Caller{Agent: role}) != nil {
		return "", true
	}
	return role, false
}

// currentFor reads the active task for status and the session-start hook. When the target has a
// role, the call names it as the caller's agent, so the server counts that role's agent-scoped
// directives (CONTRACT §2, task_current).
func currentFor(t *target) (*core.TaskCurrent, error) {
	ctx := t.ctx
	if t.role != "" {
		c, _ := core.CallerFrom(ctx)
		c.Agent = t.role
		ctx = core.WithCaller(ctx, c)
	}
	return t.svc.TaskCurrent(ctx)
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
	role, refused := roleFromEnv()
	return &target{project: project, url: url, svc: svc, ctx: ctx, role: role, roleRefused: refused}, nil
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

// reason is the text of err for a person (cli.Reason, #113): a server's message is capped
// to one line, and a local one is printed whole.
func reason(err error) string {
	return cli.Reason(err)
}

// serverSource says where the server URL came from, for a person reading status or
// doctor. CAIRN_SERVER is read first, so it is named when it is set.
func serverSource() string {
	if os.Getenv(identity.EnvServer) != "" {
		return identity.EnvServer
	}
	return identity.FileName
}
