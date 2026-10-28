# Getting started

_Status as of 2026-10-28: phase 1 (contract) is done, and its exit check passed with the operator's review of [CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review). Phase 2 (core and local store) is current. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

This page is for contributors. For what Cairn is, read [Home](Home) and [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md).

## What you need

- Go 1.25 or later. [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod) declares `go 1.25.0`, and CI reads its Go version from that file.
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
```

Run the four checks before you push. CI runs build, vet, fmt-check and test on every pull request. See [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#build-and-check) and the [Makefile](https://github.com/djm56/cairn-memory/blob/main/Makefile).

What `make test` checks today: the tests in `cmd/cairn` cover the command frame (usage, `version`, `help` and unknown commands). The tests in `internal/conformance` check that every conformance case fails against the stub, and that the recorder used to run a case stops it on a fatal error. These tests pass. No conformance case passes against a real service yet, because none exists until phase 2.

## What exists and what is later

| Area | Exists now | Planned |
|---|---|---|
| Binary | `cairn version` | `init`, `status`, `doctor`, `hook`, `export` and `mcp` in phase 3. `serve` in phase 5. `import` in phase 7. `review run` in phase 8. |
| Contract | [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md) and the types in `internal/core` | Nothing further in phase 1. Item 12 is deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)). |
| Conformance suite | `internal/conformance`, failing against the stub | Passes against the local service in phase 2. |
| Store | Nothing yet | SQLite store, migrations and full-text search in phase 2. |
| MCP adapter | Nothing yet | `cairn mcp` in phase 3. |
| Claude Code plugin | Nothing yet | Phase 4. |
| Server and web UI | Nothing yet | Phases 5 and 6. |

The phases are in [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-phases). The commands are in [DESIGN.md §5.2](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#52-commands).

## Install for end users (planned)

Phase 4 is when the plugin works. The intended steps are in the [README](https://github.com/djm56/cairn-memory/blob/main/README.md#how-it-will-be-used): install `cairn`, add the marketplace, install the plugin, and run `/cairn:init` in a project.

The README says the binary is built from source until release archives exist. [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-10--releases) puts release archives and the Homebrew tap in phase 10. The repository has no release tags yet, so building from source is the only way to get `cairn` today.

## Where next

- [Architecture](Architecture) for the shape of the code.
- [How we work](How-We-Work) for issues, branches and pull requests.
- [Roadmap](Roadmap) for the phases.
