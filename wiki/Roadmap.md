# Roadmap

_Status as of 2026-10-29: phase 1 (contract) is done, and its exit check passed with the operator's review of [CONTRACT.md §9](https://github.com/djm56/cairn-memory/blob/main/docs/CONTRACT.md#9-settled-here-for-review). Phase 2 (core and local store) is done, and its exit check passed. Phase 3 (MCP adapter) is current. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

The build runs from the outside in: the contract first, then the core, then the ways to reach it, then the operator's pages, then migration, then the review loop ([PLAN.md §2](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#2-build-order)). Version 1 is phases 0 to 4 ([PLAN.md §7](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#7-risks)).

| Phase | Name | Goal | Milestone | Status |
|---|---|---|---|---|
| 0 | [Skeleton](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-0--skeleton) | `cairn version` builds, and CI runs on an empty test suite | [milestone](https://github.com/djm56/cairn-memory/milestones) | Exit check passed |
| 1 | [Contract](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-1--contract) | The tool contract, and a conformance suite that fails against a stub | [milestone](https://github.com/djm56/cairn-memory/milestones) | Exit check passed |
| 2 | [Core and local store](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-2--core-and-local-store) | The rules in `core` over a local SQLite store | [milestone](https://github.com/djm56/cairn-memory/milestones) | Exit check passed |
| 3 | [MCP adapter](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-3--mcp-adapter) | Every tool over MCP stdio, and the commands a project needs | [milestone](https://github.com/djm56/cairn-memory/milestones) | Current |
| 4 | [Claude Code plugin](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-4--claude-code-plugin) | A plugin, a skill and a launcher, so Claude Code records a small task | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |
| 5 | [Server and remote backend](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-5--server-and-remote-backend) | `cairn serve` with API keys, and a remote client | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |
| 6 | [Web UI](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-6--web-ui) | Pages for tasks, learnings, directives and projects | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |
| 7 | [Migration from bishop-memory](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-7--migration-from-bishop-memory) | `cairn import` reads a bishop-memory database | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |
| 8 | [Review loop](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-8--review-loop) | Scheduled classify, process and grade runs | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |
| 9 | [Other clients](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-9--other-clients) | OpenCode and pi | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |
| 10 | [Releases](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#phase-10--releases) | Release archives, a Homebrew tap and plugin tags | [milestone](https://github.com/djm56/cairn-memory/milestones) | Planned |

## Two places, two jobs

- **Milestones and the board** hold live progress: which issues are open and what is in progress. The [milestones page](https://github.com/djm56/cairn-memory/milestones) and the [Projects tab](https://github.com/djm56/cairn-memory/projects) are where to look.
- **PLAN.md** holds the definitions: each phase's work and its exit check. An exit check is what a reviewer runs to say a phase is done. The full text is in [PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-phases).

The milestone descriptions are copies made from [PLAN §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-phases), held in [roadmap.json](https://github.com/djm56/cairn-memory/blob/main/.github/roadmap.json). Each one holds the phase's exit check and a link to its PLAN §3 entry, so it is not the same text as PLAN §3. When the two differ, PLAN §3 is the authority, and the two are changed together. See [How we work](How-We-Work) for how the issues and board are set up.
