# Cairn

_Status as of 2026-10-31: M1 (Server-mode MCP) is current. M2 (Server) is built; its live run (#19) is pending, so M2 is not passed. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

Cairn is an operating record for coding agents. An agent records the task it is on, the steps it takes, what happened and what it learned. The next agent reads that back before it starts. An operator turns the best lessons into rules that every agent follows.

A cairn is a pile of stones left on a trail for whoever comes next. Each task in Cairn leaves the same kind of marker.

## What Cairn does

- **Task state.** The current task, its brief, its steps and a journal. A new session continues where the last one stopped.
- **Learning.** Agents propose findings, patterns and notes about agents. The operator accepts or rejects each one. Accepted findings can become directives, which are rules agents read before they work.
- **Recall.** Before a task starts, Cairn returns the directives, lessons and similar past tasks that apply.
- **Review.** A web page to follow tasks, decide on findings and ratify directives. Scheduled review agents are optional and come later.

Cairn is one binary, `cairn`. It uses SQLite with full-text search. Clients use one central server, `cairn serve`. The operator decides: agents can only propose.

## Status

- **Phases 0 to 2:** done, and their exit checks passed.
- **M1, Server-mode MCP:** current.
- **M2, Server:** built; its live run (#19) is pending.
- **M3 to M8, and Later:** planned. See the [Roadmap](Roadmap) and the [milestones](https://github.com/djm56/cairn-memory/milestones) for live progress.
- **Version 1** is the scope of M1 to M8: a central server, the web UI and dashboard, hooks and a CLI, the Claude Code plugin, the import from bishop-memory, a pilot, and the roll-out.

## Where things live

| Where | What it holds |
|---|---|
| [The repository's docs](https://github.com/djm56/cairn-memory/tree/main/docs) | The canonical design. [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md) says what Cairn is. [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md) says what each tool does. [PLAN.md](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md) gives the milestones and their exit checks. The engineering rules are in [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md). |
| This wiki | Summaries and links. Some rules are summarised here, with a link to the doc each comes from. Where a page and a doc differ, the doc is the authority. Pages are published from `wiki/` in the repository. |
| [Issues](https://github.com/djm56/cairn-memory/issues), [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Projects board](https://github.com/djm56/cairn-memory/projects) | Live progress. Each milestone (M1 to M8, and Later) is a GitHub milestone. Epics and tasks are issues. The board tracks their status. |

## Pages

- [Getting started](Getting-Started): clone, build and check the code.
- [Architecture](Architecture): the shape of the design, with links into DESIGN.md.
- [Roadmap](Roadmap): the milestones M1 to M8 and Later, their goals, exit checks and status.
- [Decisions](Decisions): the adopted decisions D1 to D19, and the contract items the operator approved (item 12 is deferred to Later).
- [How we work](How-We-Work): issues, branches, pull requests, CI, labels and the board.
- [Glossary](Glossary): the terms Cairn uses.
