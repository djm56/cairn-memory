# Decisions

_Status as of 2026-10-28: phase 1 (contract) is done, and its exit check passed with the operator's review of [CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review). Phase 2 (core and local store) is current. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

D1 to D10 were adopted on 2026-10-27. The wording of each decision is in [PLAN.md §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions). The lines below only name each one. Reopening a decision needs the operator.

## Adopted decisions

- **D1. Vocabulary: "task".** Bishop says mission. Cairn says task. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D2. Local mode with no daemon.** `cairn mcp` opens a local SQLite file when no server is set. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D3. One `learnings` table** for findings, patterns and agent notes. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D4. Task ids** take the form `task-YYYYMMDD-NN`, from one daily sequence across projects. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D5. Step positions, not step numbers.** Display numbers are computed. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D6. One binary, `cairn`,** with sub-commands. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D7. The project file is `.cairn.toml`** at the project root. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D8. The review loop waits for phase 8.** Version 1 has no scheduled agents. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D9. Local marketplace first,** and public only after a review of the plugin's hook text and install guide. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)
- **D10. Build from the outside in,** in the order given in PLAN §2. [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)

To reopen one, open a [decision issue](https://github.com/djm56/cairn-memory/issues/new?template=decision.yml). The form asks which decision it touches, the options with their costs, and a recommendation.

## Settled in the contract

[CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review) lists the questions the design left open and how the contract answers them. Items 1 to 11 were approved by the operator on 2026-10-28. Item 12 is deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)). Only the titles are listed here.

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
12. Deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)): the step-review rule in project settings
