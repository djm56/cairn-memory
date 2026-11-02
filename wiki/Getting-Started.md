# Getting started

_Status as of 2026-10-31: M1 (Server-mode MCP) is current. M2 (Server) is built; its live run (#19) is pending, so M2 is not passed. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

This page is for contributors. For what Cairn is, read [Home](Home) and [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md).

## What you need

- Go 1.26 or later. [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod) declares `go 1.26.0`, and CI reads its Go version from that file.
- Git.
- No C toolchain. SQLite comes from `modernc.org/sqlite`, which is pure Go.

## Get the code

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
```

The clone gives you `main`. Which changes have landed is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [board](https://github.com/djm56/cairn-memory/projects).

## Build and check

```bash
make build && bin/cairn version   # builds bin/cairn; the version comes from git describe
make test                         # go test -race ./...
make vet                          # go vet ./...
make fmt-check                    # fails if a file is not gofmt-formatted
make fmt                          # gofmt -w . fixes the formatting
make mutation                     # Gremlins on internal/core and internal/store; slow, not in CI
```

Run the four checks before you push. CI runs build, vet, fmt-check and test on every pull request. See [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#build-and-check) and the [Makefile](https://github.com/djm56/cairn-memory/blob/main/Makefile).

What `make test` checks today: the tests in `cmd/cairn` cover the command frame (usage, `version`, `help` and unknown commands), and each M1 command against an in-process server: `init`, `status`, `doctor`, `export task` and `hook session-start`. The stdio flow test drives a whole task through `cairn mcp` and asserts the result of each call. The tests in `internal/conformance` check that every conformance case fails against `core.Unimplemented`, the stub, and that the recorder used to run a case stops it on a fatal error. These tests pass. The tests in `internal/core` run every conformance case against `core.Local`, the local service over SQLite. `core.Local` passes all 93. The tests in `internal/store` include the two-process task-id test.

## What exists and what is later

| Area | Exists now | Planned |
|---|---|---|
| Binary | `cairn version`, `cairn mcp` (server only), `cairn init`, `status`, `doctor`, `export task`, `hook session-start` (server only), and in M2 `cairn serve`, `cairn key`, `cairn backup` and `cairn restore` | `enable` and the other hooks in M4. `import` in M6. `review run` is Later. |
| Contract | [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md) and the types in `internal/core` | The M1 tool changes are in the code. Item 12 is deferred to Later ([#41](https://github.com/djm56/cairn-memory/issues/41)). |
| Conformance suite | `internal/conformance`: 93 cases, all passing against the local service and over the HTTP client | — |
| Store | SQLite store with migrations, full-text search and task-id allocation (phase 2) | Postgres is Later. |
| MCP adapter | `cairn mcp`: the 15 tools over stdio, to the server named by `server` in `.cairn.toml` or `CAIRN_SERVER`. It refuses to start without one. | — |
| Claude Code plugin | Nothing yet | M5. |
| Server | `cairn serve` is built in M2: TLS by files or behind a proxy, API keys with three scopes, backup and restore. Its live run (#19) is pending. The setup is in [DEPLOY.md](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md). | The web UI and dashboard in M3. |
| CLI and hooks | `cairn hook session-start` (M1), and the M1 commands in the Binary row | `internal/cli` (the CLI over the whole API, with `--json`), `pre-compact` and `stop`, in M4. |

The milestones are in [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-milestones). The commands are in [DESIGN.md §5.2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#52-commands).

## Install for end users (planned)

M5 is when the plugin works. The intended steps are in the [README](https://github.com/djm56/cairn-memory/blob/main/README.md#how-it-will-be-used): install `cairn`, add the marketplace, install the plugin, and run `/cairn:init` in a project.

The README says the binary is built from source until release archives exist. [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#milestone-table) puts release archives and the Homebrew tap in Later. The repository has no release tags yet, so building from source is the only way to get `cairn` today.

## Where next

- [Architecture](Architecture) for the shape of the code.
- [How we work](How-We-Work) for issues, branches and pull requests.
- [Roadmap](Roadmap) for the milestones.
