# Architecture

_Status as of 2026-10-28: phase 1 (contract) is built and waits on the operator's review of [CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review). Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

Parts that are not built yet are marked in [Getting started](Getting-Started#what-exists-and-what-is-later).

This page summarises [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md). Each section links to the part of the design it comes from. DESIGN.md and PLAN.md are the contract for this repository ([CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#changes-to-the-design)), so where this page and the design differ, the design is right and this page needs fixing.

## One binary

`cairn` is the only binary. It serves as the MCP server, the HTTP server with the web UI, the hook helper and the admin tool. It is written in Go, uses SQLite with full-text search, and needs no C toolchain.

_Design: [DESIGN.md §1.1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#11-principles)._

## Local and server mode

The same code runs in two modes:

- **Local.** `cairn mcp` opens a SQLite file on the machine. No daemon is needed (decision D2).
- **Server.** `cairn mcp` sends each call to `cairn serve` with an API key. This suits a home server, a team, or several machines.

Switching modes is one line in the project file or one environment variable. The database path and the switch are described in the design.

_Design: [DESIGN.md §5.3](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#53-local-and-remote-backends)._

## Layers

An MCP client talks to `cairn mcp` over stdio. The adapter calls the `Service` interface. The local backend writes to the store. The remote backend calls `cairn serve`, which calls the same core. The packages:

- `internal/core`: the `Service` interface and every rule, including the close policy, status moves and operator-only changes.
- `internal/store`: SQLite access, migrations and full-text search. It is the only package with SQL.
- `internal/api`: HTTP handlers. They decode the request, call core and encode the result.
- `internal/client`: a `Service` over HTTP, used by the remote backend.
- `internal/mcp`: the MCP tools. Each one calls `Service`.
- `internal/identity`: finds the project, agent and session for a call.
- `internal/auth`: API keys and scopes in server mode.
- `internal/ui`: the web pages, embedded in the binary.
- `cmd/cairn`: the one binary and its sub-commands.

`internal/conformance` holds the test suite that checks any `Service`. It is covered below.

_Design: [DESIGN.md §5.1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#51-packages)._

## Data model

SQLite, through `modernc.org/sqlite`. Every table carries the project. Times are UTC and set by the server. Schema changes are numbered migrations that run at start-up.

_Design: [DESIGN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#3-data-model)._

## Tool surface (MCP)

Tool names are short and have no prefix. The client adds the server prefix. Agents write and read through these tools. The operator decides on what agents propose.

The list of tools is in the design. The fields, errors and rules of each tool are in the contract.

_Design: [DESIGN.md §4](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#4-tool-surface-mcp) and [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md)._

## The contract and the conformance suite

`docs/CONTRACT.md` is the call-level contract. `internal/core` is its Go form. `internal/conformance` is a test suite that takes any `Service` and checks the behaviour the contract describes. A behaviour exists when the suite says so. The three change together.

_Design: [DESIGN.md §5.1](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#51-packages) and [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md)._

## Clients

Claude Code comes first, as a plugin in phase 4. OpenCode and pi follow in phase 9. Any MCP client can run `cairn mcp` over stdio. Bishop will become a crew pack in its own repository.

_Design: [DESIGN.md §6](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#6-clients)._

## Security

Local mode listens on nothing. `cairn serve` binds to loopback by default, and it refuses any other address unless at least one API key exists. Keys are stored hashed. An agent key cannot reach operator routes.

_Design: [DESIGN.md §8](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#8-security)._

## The review loop (later)

The review loop is optional and comes in phase 8. Scheduled agents classify findings and grade finished tasks. The operator decides on their recommendations.

_Design: [DESIGN.md §7](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#7-review-loop-optional-later)._

## Engineering rules

These come from [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#engineering-rules) and [PLAN.md §6](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#6-engineering-rules). The short form:

- Rules live in `internal/core`. The conformance suite is the gate.
- SQL lives only in `internal/store`.
- Every tool result is read by a model, so results stay small and paged.
- Tests come before code for each tool, through the conformance suite.
- Mutation testing covers `core` and `store` from phase 2.
- A missing project is an error that says how to fix it. There is no default.
- Docs are dated, and their claims carry evidence.
