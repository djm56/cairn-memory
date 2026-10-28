# Cairn Memory

A memory for coding agents. Agents record the task they are on, the steps
they take, what happened and what they learned. The next agent reads that
back before it starts. An operator turns the best lessons into rules that
every agent follows.

A cairn is a pile of stones left on a trail for whoever comes next. Each task
in Cairn leaves the same kind of marker.

<!-- cairn-phase: 1 -->

> **Status as of 2026-10-28:** phase 1 (contract) is built and waits on the
> operator's review of [CONTRACT.md §9](docs/CONTRACT.md#9-settled-here-for-review).
> Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones)
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

- **Works with any agent harness.** Plain Claude Code first; OpenCode, pi,
  Bishop and others through the same tools. Nothing in the core assumes a
  framework, a crew or a file layout.
- **The database is the source of truth.** Agents write and read through MCP
  tools. No Markdown state files to keep in step.
- **One binary, `cairn`.** It is the MCP server, the HTTP server and web UI,
  the hook helper and the admin tool. Go and SQLite with full-text search;
  no C toolchain.
- **No server needed to start.** On one machine, `cairn mcp` uses a local
  database. Point it at `cairn serve` when you want a home server or a team.
- **The operator decides.** Agents can only propose. Accepting a finding or
  ratifying a directive is an operator action.

## How it will be used

Until release archives exist (phase 10 of the plan), build the binary from
source with `make build`; see [CONTRIBUTING.md](CONTRIBUTING.md). The plugin
flow arrives at phase 4. Once phase 4 is done, the intended steps are:

```bash
# With cairn on PATH, in Claude Code:
/plugin marketplace add djm56/cairn-memory
/plugin install cairn@cairn-memory

# In a project:
/cairn:init          # writes .cairn.toml with this project's name
```

From then on, each session starts with the active task in context, and the
`cairn` skill tells the agent how to start, step through and close a task.

`.cairn.toml` is committed to the project:

```toml
project = "acme-api"
# server = "https://cairn.lan:8787"   # optional; absent means local
```

## Documentation

| Document | What it covers |
|---|---|
| [docs/DESIGN.md](docs/DESIGN.md) | Concepts, data model, the MCP tools, architecture, clients, security |
| [docs/CONTRACT.md](docs/CONTRACT.md) | Every tool's inputs, outputs, errors and rules; the operator calls |
| [docs/PLAN.md](docs/PLAN.md) | Build phases with exit checks, what is ported from bishop-memory, migration, decisions |
| [CHANGELOG.md](CHANGELOG.md) | What changed, by pull request, under Unreleased |
| [The wiki](https://github.com/djm56/cairn-memory/wiki) | Summaries, the roadmap in plain words, and how the work is run. Published from `wiki/` |

## Project management

- [Wiki](https://github.com/djm56/cairn-memory/wiki): summaries and how the work is run.
- [Milestones](https://github.com/djm56/cairn-memory/milestones): one per phase.
- [Issues](https://github.com/djm56/cairn-memory/issues): the work, from forms.
- [Projects](https://github.com/djm56/cairn-memory/projects): the board that tracks each issue.
- [CHANGELOG.md](CHANGELOG.md): what each pull request changed.

## Licence

MIT, as bishop-memory. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
