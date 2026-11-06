# Cairn

_Status as of 2026-11-06: M5 (Plugin) is current. The exit checks for M1 (the MCP stdio flow test), M2 (the live run, #19) and M3 (the web UI exit-check test) have passed. M4 (hooks and CLI) passed its /compact hand check by operator ruling, recorded 2026-11-06; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) have not been run, and #206 is open. Phases 0 to 2 are done. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

Cairn is an operating record for coding agents. An agent records the task it is on, the steps it takes, what happened and what it learned. The next agent reads that back before it starts. An operator turns the best lessons into rules that every agent follows.

A cairn is a pile of stones left on a trail for whoever comes next. Each task in Cairn leaves the same kind of marker.

## What Cairn does

- **Task state.** The current task, its brief, its steps and a journal. A new session continues where the last one stopped.
- **Learning.** Agents propose findings, patterns and notes about agents. The operator accepts or rejects each one. Accepted findings can become directives, which are rules agents read before they work.
- **Recall.** Before a task starts, Cairn returns the directives, lessons and similar past tasks that apply.
- **Review.** A web page to follow tasks, decide on findings and ratify directives. Scheduled review agents are optional and come later.

Cairn is one binary, `cairn`. It uses SQLite with full-text search. Clients talk to one central server, `cairn serve`, over HTTP with an API key. The operator decides: agents can only propose.

## Start here

Pick the goal you have.

| Goal | Read |
|---|---|
| Try Cairn on your own machine | [Quick-Start](Quick-Start), then [Install-Local](Install-Local) for each step in detail |
| Run a server that other machines reach | [Install-Server](Install-Server) |
| Upgrade a server or a local install that is already running | [Upgrade](Upgrade) |
| Connect Claude Code, opencode or pi | [Connecting-Agents](Connecting-Agents) |
| Check that an install works, or find out why a check failed | [Checks](Checks), and [Troubleshooting](Troubleshooting) |
| Look up a command, flag, tool or exit code | [CLI-Reference](CLI-Reference), [MCP-Tools](MCP-Tools), [Binaries](Binaries) |
| Work on the code | [Getting-Started](Getting-Started) |

## Where each topic lives

| Topic | Page |
|---|---|
| Building the binary, and the Makefile targets | [Binaries](Binaries) |
| Keys, roles, backup, restore and logs | [Operations](Operations) |
| What each `cairn` command does, and its exit codes | [CLI-Reference](CLI-Reference) |
| What each MCP tool takes and returns | [MCP-Tools](MCP-Tools) |
| How the design fits together | [Architecture](Architecture) |
| The milestones and their exit checks | [Roadmap](Roadmap) |
| The adopted and pending decisions | [Decisions](Decisions) |
| How issues, pull requests and the wiki are run | [How-We-Work](How-We-Work) |
| The terms Cairn uses | [Glossary](Glossary) |

The design and the contract are in the repository. This wiki summarises them, and says where each rule comes from. Where a page and a document differ, the document is the authority: [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md) says what Cairn is, [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md) says what each tool does, and [PLAN.md](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md) gives the milestones and their exit checks. The rules for contributors are in [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md).

## Status

- **Phases 0 to 2:** done, and their exit checks passed.
- **M1, Server-mode MCP:** exit check passed (MCP stdio flow test, recorded 2026-11-03).
- **M2, Server:** exit check passed on 2026-11-02 (live run, #19).
- **M3, Web UI and dashboard:** exit check passed (web UI exit-check test, recorded 2026-11-03).
- **M4, Hooks and CLI:** exit check passed by operator ruling (the /compact hand check, recorded 2026-11-06). Sections 4 and 5 of the guide are not run, and #206 (the hook text) is open under M5.
- **M5, Plugin:** current. The Claude Code plugin is not shipped yet. Until it is, Claude Code is connected by hand, as [Connecting-Agents](Connecting-Agents) describes.
- **M6 to M8, and Later:** planned. See the [Roadmap](Roadmap).

## Pages

- [Quick-Start](Quick-Start): the shortest correct path on one machine, with a "you should see" line for each step.
- [Install-Local](Install-Local): the developer install in detail: prerequisites, files, environment variables and checks.
- [Install-Server](Install-Server): a server other machines reach, with TLS by files or behind a proxy.
- [Upgrade](Upgrade): upgrading a running server or a local install, and rolling back.
- [Binaries](Binaries): every Makefile target, version strings, supported systems and where the binary goes.
- [CLI-Reference](CLI-Reference): the 21 verbs of `cairn`, with their flags, environment and exit codes.
- [MCP-Tools](MCP-Tools): the 15 tools `cairn mcp` serves, one section each.
- [Connecting-Agents](Connecting-Agents): Claude Code, opencode (experimental), pi (experimental), and the hooks.
- [Operations](Operations): keys and roles, backup, restore, logs and common tasks.
- [Checks](Checks): the checks an operator can run, what each means, and the CI workflows.
- [Troubleshooting](Troubleshooting): symptoms, their causes and their fixes.
- [Architecture](Architecture): the shape of the design, with links into DESIGN.md.
- [Roadmap](Roadmap): the milestones M1 to M8 and Later, with their exit checks and status.
- [Decisions](Decisions): the adopted decisions, and the ones still waiting for the operator.
- [How-We-Work](How-We-Work): issues, branches, pull requests, CI, labels and the board.
- [Glossary](Glossary): the terms Cairn uses.
- [Getting-Started](Getting-Started): for contributors: the source, the build and the checks.
