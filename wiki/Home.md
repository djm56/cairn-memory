# Cairn

_Status as of 2026-10-28: phase 1 (contract) is built and waits on the operator's review of [CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review). Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

Cairn is a memory for coding agents. An agent records the task it is on, the steps it takes, what happened and what it learned. The next agent reads that back before it starts. An operator turns the best lessons into rules that every agent follows.

A cairn is a pile of stones left on a trail for whoever comes next. Each task in Cairn leaves the same kind of marker.

## What Cairn does

- **Task state.** The current task, its brief, its steps and a journal. A new session continues where the last one stopped.
- **Learning.** Agents propose findings, patterns and notes about agents. The operator accepts or rejects each one. Accepted findings can become directives, which are rules agents read before they work.
- **Recall.** Before a task starts, Cairn returns the directives, lessons and similar past tasks that apply.
- **Review.** A web page to follow tasks, decide on findings and ratify directives. Scheduled review agents are optional and come in phase 8.

Cairn is one binary, `cairn`. It uses SQLite with full-text search. On one machine no server is needed to start. The operator decides: agents can only propose.

## Status

- **Phase 0, Skeleton:** exit check passed.
- **Phase 1, Contract:** built. It waits for the operator's review of [CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review).
- **Phases 2 to 10:** planned. See the [Roadmap](Roadmap) and the [milestones](https://github.com/djm56/cairn-memory/milestones) for live progress.
- **Version 1** is phases 0 to 4: a local memory that a plain Claude Code project can use.

## Where things live

| Where | What it holds |
|---|---|
| [The repository's docs](https://github.com/djm56/cairn-memory/tree/main/docs) | The canonical design. [DESIGN.md](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md) says what Cairn is. [CONTRACT.md](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md) says what each tool does. [PLAN.md](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md) gives the phases and their exit checks. The engineering rules are in [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md). |
| This wiki | Summaries and links. Some rules are summarised here, with a link to the doc each comes from. Where a page and a doc differ, the doc is the authority. Pages are published from `wiki/` in the repository. |
| [Issues](https://github.com/djm56/cairn-memory/issues), [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Projects board](https://github.com/djm56/cairn-memory/projects) | Live progress. Each phase is a milestone. Epics and tasks are issues. The board tracks their status. |

## Pages

- [Getting started](Getting-Started): clone, build and check the code.
- [Architecture](Architecture): the shape of the design, with links into DESIGN.md.
- [Roadmap](Roadmap): the eleven phases, their goals and their status.
- [Decisions](Decisions): the adopted decisions D1 to D10, and the contract items awaiting review.
- [How we work](How-We-Work): issues, branches, pull requests, CI, labels and the board.
- [Glossary](Glossary): the terms Cairn uses.
