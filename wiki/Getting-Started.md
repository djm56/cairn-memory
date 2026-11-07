# Getting started (contributors)

_Status as of 2026-11-06: M5 (Plugin) is current. The exit checks for M1 (the MCP stdio flow test), M2 (the live run, #19) and M3 (the web UI exit-check test) have passed. M4 (hooks and CLI) passed its /compact hand check by operator ruling, recorded 2026-11-06; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) have not been run, and #206 is open. Phases 0 to 2 are done. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

This page is for people who change the code. If you want to use Cairn, start with [Quick-Start](Quick-Start). For what Cairn is, read [Home](Home) and [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md).

## What you need

- **Go 1.26 or later.** [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod) declares `go 1.26.0`, and CI reads its Go version from that file.
- **Git**, for the clone, and for the version stamp that `make build` takes from `git describe`.
- **No C toolchain.** SQLite comes from `modernc.org/sqlite`, which is pure Go.

## Get the code

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
```

The clone gives you `main`. For a change, branch from `main`, as [How-We-Work](How-We-Work) describes. Which changes have landed is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [board](https://github.com/djm56/cairn-memory/projects).

## Build and check

```bash
make build                 # bin/cairn, version from git describe
bin/cairn version
make test                  # go test -race ./...
make vet                   # go vet ./...
make fmt-check             # fails if a file is not gofmt-formatted
make fmt                   # gofmt -w . rewrites the files
make scan-invisible        # fails on bidirectional or invisible characters
make scan-local-paths      # fails on a local user or machine path
make cross                 # compiles and vets for Windows, macOS, FreeBSD and OpenBSD
make mutation              # Gremlins on internal/core and internal/store; slow, not in CI
```

Run the checks before you push. CI runs `build`, `vet`, `fmt-check`, `test`, `cross`, `scan-invisible` and `scan-local-paths` on every pull request. [Binaries](Binaries) describes each target, and [Checks](Checks) lists the scripts and the drift tests.

## What the tests cover

- **`cmd/cairn`** tests the command frame (usage, `version`, `help` and unknown commands), each command against an in-process server, the hooks, `enable`, `doctor`, the exports and the CLI verbs. The stdio flow test drives a whole task through `cairn mcp`.
- **`internal/conformance`** holds the suite that checks any `Service` against the contract. It has **106 cases**. The suite is run against four implementations, and the results are these, measured for this page:
  - `core.Local`, the local service over SQLite (`internal/core`): all 106 pass.
  - The HTTP client (`internal/client`): all 106 pass.
  - The listener of `cairn serve`, reached over HTTP (`internal/server`): all 106 pass.
  - The CLI (`internal/conformance/cliservice`): **98 pass and 8 are deferred**. Three of the eight are the empty step reference, which decision D30 keeps out of the CLI. Two are the learning list over all projects, which D31 keeps out of the CLI. The other three are identity fields the CLI does not send: its client name, its session, and a viewer's role, which decision D33 keeps out of the CLI; the reasons are in `internal/conformance/cliservice/suite_test.go`.
- **`internal/store`** includes the two-process task-id test.
- **`internal/mcp` and `cmd/cairn`** include the drift tests, which check [MCP-Tools](MCP-Tools) and [CLI-Reference](CLI-Reference) against the code.
- **`internal/smokecheck`** runs `scripts/smoke-check.sh` against a stub binary and a stub server.

## Where the code is

The packages, and what each one holds, are in [Architecture](Architecture#layers). In short:

- `internal/core` holds every rule. `internal/store` is the only package with SQL.
- `internal/api` holds the HTTP handlers, and `internal/client` is the HTTP client.
- `internal/mcp` holds the MCP tools, and `cmd/cairn` holds the one binary.
- `internal/conformance` is the gate for behaviour, and `internal/ui` holds the web pages.

## Before you open a pull request

- Read [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md). It has the workflow, the engineering rules and the rules for keeping docs current.
- Keep `DESIGN.md`, `CONTRACT.md`, `internal/core` and `internal/conformance` in step, in one pull request, when a contract changes.
- Add a `CHANGELOG.md` entry under Unreleased, unless the pull request carries the `skip-changelog` label.
- Edit the wiki in `wiki/`, not on the GitHub wiki. A change to user-facing behaviour updates the matching page.

## Where next

- [Architecture](Architecture) for the shape of the design and the code.
- [How-We-Work](How-We-Work) for issues, branches, pull requests and the board.
- [Checks](Checks) for the scripts and the CI workflows.
- [Roadmap](Roadmap) for the milestones.
