# Cairn Memory

An operating record for coding agents. Agents record the task they are on, the steps
they take, what happened and what they learned. The next agent reads that
back before it starts. An operator turns the best lessons into rules that
every agent follows.

A cairn is a pile of stones left on a trail for whoever comes next. Each task
in Cairn leaves the same kind of marker.

<!-- cairn-milestone: M1 -->

> **Status as of 2026-10-30:** M1 (Server-mode MCP) is current. Phases 0 to 2
> are done, and their exit checks passed. Live progress is on the
> [milestones](https://github.com/djm56/cairn-memory/milestones)
> and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).
>
> Read [docs/DESIGN.md](docs/DESIGN.md) for what Cairn is, and
> [docs/PLAN.md](docs/PLAN.md) for the build order. Cairn replaces
> [bishop-memory](https://github.com/djm56/bishop-memory), which keeps
> running until the move is done.

[![CI](https://github.com/djm56/cairn-memory/actions/workflows/ci.yml/badge.svg)](https://github.com/djm56/cairn-memory/actions/workflows/ci.yml)

## What it does

- **Task state.** The current task, its brief, its plan, its steps and a
  journal. A new session continues where the last one stopped.
- **Learning.** Agents propose findings, patterns and notes about agents.
  The operator accepts or rejects each finding. Accepted findings become
  **directives**: rules agents read before they work, in one project or all
  of them.
- **Recall.** Before a task starts, Cairn returns the directives, lessons and
  similar past tasks that apply.
- **Review.** A web page to follow tasks, decide on findings and ratify
  directives. Later, optional scheduled agents classify findings and grade
  finished tasks.

## Design in brief

- **Works with any agent harness.** Plain Claude Code first, through MCP; OpenCode,
  pi, Bishop and others through the CLI (planned for M4) over the same HTTP API (D14). Nothing in the core assumes a
  framework, a crew or a file layout.
- **The database is the source of truth.** Agents write and read through MCP
  tools. No Markdown state files to keep in step.
- **One binary, `cairn`.** It is the MCP server (`cairn mcp`, built in M1),
  the HTTP server and web UI (`cairn serve`, planned for M2), the hook helper
  and the admin tool. The M1 commands are `init`, `status`, `doctor`,
  `export task` and `hook session-start`; the CLI proper is planned for M4. Go
  and SQLite with full-text search; no C toolchain.
- **One server.** Clients connect to `cairn serve` over HTTP with an API key,
  so a team or several machines share one place to look and back up (decision
  D11). `cairn serve` is planned for M2. `cairn mcp` and the M1 commands need
  `server` in `.cairn.toml` or `CAIRN_SERVER`, and `cairn mcp` refuses to start
  without one. `cairn init` writes that file in the project directory, and does
  not contact the server: `cairn init --project <name> --server <url>`. The
  server URL may not carry a user name or password, since the file is committed.
- **The operator decides.** Agents can only propose. Accepting a finding or
  ratifying a directive is an operator action.

## How it will be used

Until release archives exist (Later in the plan), build the binary from
source with `make build`; see [CONTRIBUTING.md](CONTRIBUTING.md). The plugin
flow arrives in M5. Once M5 is done, the intended steps are:

```bash
# With cairn on PATH, in Claude Code:
/plugin marketplace add djm56/cairn-memory
/plugin install cairn@cairn-memory

# In a project:
/cairn:init          # writes .cairn.toml with this project's name
```

From then on, each session starts with the active task in context, and the
`cairn` skill tells the agent how to start, step through and close a task.

`.cairn.toml` is committed to the project. `server` is required (D11); the
API key does not go in this file, and neither does a user name or password in
the server URL.

```toml
project = "acme-api"
server = "https://cairn.lan:8788"
```

The repository and the Go module still carry the name `cairn-memory`.
Renaming them to `cairn` is planned (decision D18), and the rename has not
been done.

### Running the server

`cairn serve` is the server, and the only listener in the binary. It keeps its
data in the SQLite file named by `--db`, which is created if it does not exist.
It listens on loopback at `127.0.0.1:8788` unless `--addr` says otherwise:

```bash
cairn serve --db ./cairn.db
```

An address other than loopback needs TLS, by `--tls-cert` and `--tls-key`, or
`--behind-proxy` when a proxy in front of the server terminates TLS. It also
needs at least one active API key. Creating keys with `cairn key add` is not
built yet, so a server with no key refuses every call, and a non-loopback server
cannot start until keys can be made. `GET /healthz` answers a liveness check
without a key. The bind rules and the limits are in
[DESIGN.md §5.3](docs/DESIGN.md#53-the-server).

## Documentation

| Document | What it covers |
|---|---|
| [docs/DESIGN.md](docs/DESIGN.md) | Concepts, data model, the MCP tools, architecture, clients, security |
| [docs/CONTRACT.md](docs/CONTRACT.md) | Every tool's inputs, outputs, errors and rules; the operator calls |
| [docs/PLAN.md](docs/PLAN.md) | Build order: milestones M1 to M8 and Later with exit checks, what is guided by bishop-memory, migration, decisions |
| [CHANGELOG.md](CHANGELOG.md) | What changed, by pull request, under Unreleased |
| [The wiki](https://github.com/djm56/cairn-memory/wiki) | Summaries, the roadmap in plain words, and how the work is run. Published from `wiki/` |

## Project management

- [Wiki](https://github.com/djm56/cairn-memory/wiki): summaries and how the work is run.
- [Milestones](https://github.com/djm56/cairn-memory/milestones): one per milestone, M1 to M8 and Later.
- [Issues](https://github.com/djm56/cairn-memory/issues): the work, from forms.
- [Projects](https://github.com/djm56/cairn-memory/projects): the board that tracks each issue.
- [CHANGELOG.md](CHANGELOG.md): what each pull request changed.

## Licence

MIT, as bishop-memory. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
