# Decisions

_Status as of 2026-10-30: M1 (Server-mode MCP) is current. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

D1 to D10 were adopted on 2026-10-27, and D11 to D18 on 2026-10-30. The wording of each decision is in [PLAN.md §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions). The lines below only name each one. Reopening a decision needs the operator.

## Adopted decisions

- **D1. Vocabulary: "task".** Bishop says mission. Cairn says task. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D2. Local mode with no daemon.** Superseded by D11. The local database path was cut before it was built. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D3. One `learnings` table** for findings, patterns and agent notes. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D4. Task ids** take the form `task-YYYYMMDD-NN`, from one daily sequence across projects. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D5. Step positions, not step numbers.** Display numbers are computed. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D6. One binary, `cairn`,** with sub-commands. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D7. The project file is `.cairn.toml`** at the project root. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D8. The review loop waits for the Later set.** Superseded by D17. Version 1 has no scheduled agents. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D9. Local marketplace first,** and public only after a review of the plugin's hook text and install guide. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D10. Build from the outside in.** Superseded by the M1 to M8 order in PLAN §2 and §3. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)

## Plan v1 decisions

- **D11. Central server only.** `cairn mcp` and the CLI require a `server` in `.cairn.toml` or `CAIRN_SERVER`; there is no local database path for clients. Replaces D2. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D12. SQLite stays, behind `Service` and `store`.** Postgres is a second store later, gated by the conformance suite. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D13. Three key scopes:** `agent` (tools), `operator` (decisions and ratification) and `viewer` (read-only UI and dashboard). Replaces the two agent and operator scopes and the admin scope in DESIGN §8. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D14. Two agent surfaces.** MCP for Claude Code; a CLI over the same HTTP API for every other harness. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D15. Hooks on `session-start`, `pre-compact` and `stop`,** installed by `cairn enable --agent`. Extends the session-start hook in DESIGN §4.1. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D16. Directives capped at 12 per call,** ranked agent, then project, then global, and then newest. Each has a `review_by` date, 90 days by default. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D17. No scheduled review agents in v1.** The review loop moves to Later, and the operator reviews proposed findings daily from the UI. Replaces D8. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D18. The repository, binary and plugin are named `cairn`,** and the category line is "operating record for coding agents". Replaces "cairn-memory". The rename is deferred: its timing is still to be decided. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)

To reopen one, open a [decision issue](https://github.com/djm56/cairn-memory/issues/new?template=decision.yml). The form asks which decision it touches, the options with their costs, and a recommendation.

## Settled in the contract

[CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review) lists the questions the design left open and how the contract answers them. Items 1 to 11 were approved by the operator on 2026-10-28. Item 12 is deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)), which Plan v1 moves to Later. Items 13 and later are Bishop's defaults, written during phase 2, and they await the operator's approval. They are not listed here until approved. Only the titles of items 1 to 12 are listed here.

1. Operator calls are part of `Service`
2. The project is needed for reads too
3. `task_start` makes the task active by default
4. `blocked` and `abandoned` need a reason
5. The brief is frozen once any step starts
6. Agents cannot log `started`, `finished`, `review` or `close` events
7. Step refs are an object
8. `fail` and `skip` need a summary
9. `missing` uses fixed keys
10. Rows in another project are `not_found`
11. Agent-scoped directives apply in every project
12. Deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)), now Later: the step-review rule in project settings
