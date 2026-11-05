# Getting started

_Status as of 2026-11-03: M4 (Hooks + CLI) is current, and the exit checks for M1 (the MCP stdio flow test), M2 (the live run, #19) and M3 (the web UI exit-check test) have passed. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

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

What `make test` checks today: the tests in `cmd/cairn` cover the command frame (usage, `version`, `help` and unknown commands), and each M1 command against an in-process server: `init`, `status`, `doctor`, `export task` and `hook session-start`. They also cover the M2 commands, and the M4 ones: the hooks, `enable`, `doctor --agent`, `export project` and the CLI verbs. The stdio flow test drives a whole task through `cairn mcp` and asserts the result of each call. The tests in `internal/conformance` check that every conformance case fails against `core.Unimplemented`, the stub, and that the recorder used to run a case stops it on a fatal error. These tests pass. The tests in `internal/core` run every conformance case against `core.Local`, the local service over SQLite. `core.Local` passes all 106. The HTTP client passes all 106, and the CLI passes 98, with the other 8 deferred and named. The tests in `internal/store` include the two-process task-id test.

## What exists and what is later

| Area | Exists now | Planned |
|---|---|---|
| Binary | `cairn version`, `cairn mcp` (server only), `cairn init`, `status`, `doctor`, `export task`, `hook session-start` (server only), and in M2 `cairn serve`, `cairn key`, `cairn backup` and `cairn restore`; and in M4 the hooks, `enable`, `doctor --agent`, `export project` and the CLI verbs | `import` in M6. `review run` is Later. |
| Contract | [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md) and the types in `internal/core` | The M1 tool changes are in the code. Item 12 is deferred to Later ([#41](https://github.com/djm56/cairn-memory/issues/41)). |
| Conformance suite | `internal/conformance`: 106 cases, all passing against the local service and over the HTTP client; through the CLI, 98 pass and 8 are deferred | — |
| Store | SQLite store with migrations, full-text search and task-id allocation (phase 2) | Postgres is Later. |
| MCP adapter | `cairn mcp`: the 15 tools over stdio, to the server named by `server` in `.cairn.toml` or `CAIRN_SERVER`. It refuses to start without one. | — |
| Claude Code plugin | Nothing yet | M5. |
| Server | `cairn serve` is built in M2: TLS by files or behind a proxy, API keys with three scopes, backup and restore. Its live run (#19) passed on 2026-11-02. The web UI at `/ui/` is built in M3: sign-in, the read pages and the actions. The setup is in [DEPLOY.md](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md). | — |
| CLI and hooks | The `pre-compact` and `stop` hooks, and `enable --agent claude-code`, are built in M4; `session-start` came in M1. `opencode` and `pi` are EXPERIMENTAL. The CLI verbs over the whole API, with `--json`, are built in M4 (#87). | The M4 exit check: a task carried across `/compact` on Claude Code, which has not yet been run. See [the hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md). |

The CLI verbs are `task` (`current`, `get`, `list`, `start`, `update`, `close`), `step` (`add`, `update`, `plan`), `event log`, `learning` (`add`, `list`), `directive list`, `recall`, `search` and `prime`. Each takes `--json`, which writes one line of JSON. A list with nothing in it prints `no <items>`, such as `no tasks` or `no directives`. The exit codes of a failed call are in [DESIGN.md §5.2.2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#522-exit-codes-of-the-cli-verbs-m4-88).

The milestones are in [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-milestones). The commands are in [DESIGN.md §5.2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#52-commands).

## Install for end users (planned)

M5 is when the plugin works. The intended steps are in the [README](https://github.com/djm56/cairn-memory/blob/main/README.md#how-it-will-be-used): install `cairn`, add the marketplace, install the plugin, and run `/cairn:init` in a project.

The README says the binary is built from source until release archives exist. [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#milestone-table) puts release archives and the Homebrew tap in Later. The repository has no release tags yet, so building from source is the only way to get `cairn` today.

## Where next

- [Architecture](Architecture) for the shape of the code.
- [How we work](How-We-Work) for issues, branches and pull requests.
- [Roadmap](Roadmap) for the milestones.
