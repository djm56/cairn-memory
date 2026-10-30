# Cairn Memory — Build Plan

<!-- cairn-phase: 3 -->

**Status as of 2026-10-29: phase 1 (contract) is done, and its exit check
passed with the operator's review of [CONTRACT.md](CONTRACT.md) §9. Phase 2
(core and local store) is done, and its exit check passed. Phase 3
(MCP adapter) is current. Live progress is on the
[milestones](https://github.com/djm56/cairn-memory/milestones) and the
[Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).
The rule for this line is in §9.**

What Cairn is and how it works is in [DESIGN.md](DESIGN.md).

## 1. Where this comes from

Cairn replaces [bishop-memory](https://github.com/djm56/bishop-memory) (a local
bishop-memory checkout). bishop-memory works, and it holds the
operator's history: missions, findings, directives and grades. It was built
around one harness, Bishop, and around one idea: the harness writes Markdown
files and the service imports them.

Two plans in bishop-memory led here:

- `docs/plans/HARNESS-PORTABILITY-PLAN.md` set out to make bishop-memory work
  with other harnesses. Its inventory (§2) found the core generic and the
  Bishop coupling at the edges: the importer, the reconciler, the Markdown
  parsers, the MCP adapter's names, and the harness-side hooks.
- `docs/plans/MEMORY-PLUGIN-PLAN.md` (2026-10-27) proposed a third mode where
  state lives only in the service, shipped as a plugin. Once state lives in
  the service, the importer, the parsers and the reconciler are not needed.

Removing those edges from bishop-memory while it stays compatible with the live
Bishop harnesses would cost more than building the core again, cleanly, and
writing the parts that are already good fresh, guided by bishop-memory. That is
this plan.

### 1.1 What is guided by bishop-memory, and what is not

From bishop-memory, by size (non-test lines at `4c170b6`):

| bishop-memory | Lines | In Cairn |
|---|---|---|
| `internal/importer` (Markdown → rows) | 1,863 | **Dropped.** Agents write through tools |
| `internal/debrief`, `criteria`, `review` (heading and verdict parsers) | 1,016 | **Dropped.** The same data arrives as tool fields; the parsers' test cases become validation cases |
| `scripts/reconcile-memory.py`, `export-decisions.py`, `push-memory.py` | ~2,300 | **Dropped** |
| Harness hooks and connect script (bishop-harness) | ~1,200 | **Dropped** |
| `internal/store` (SQLite, migrations, FTS) | 929 | **Written fresh**, guided by bishop-memory (for reference: `internal/store/sqlite.go`, `migrate.go`, `db/schema.sql`), with typed queries for every table |
| `internal/api` (HTTP) | 7,726 | **Written fresh in part**, guided by bishop-memory. Handlers become thin over `core`; inline SQL moves to `store` |
| `cmd/mcpd` (MCP adapter) | 2,090 | **Rewritten** over the `Service` interface; the HTTP-error and output-shaping helpers are written fresh, guided by `cmd/mcpd/main.go` |
| `internal/auth`, `middleware`, `config` | 588 | **Written fresh**, guided by bishop-memory (for reference: `internal/auth/keys.go`) |
| `internal/ui` (HUD, triage, crew, performance pages) | 92 + HTML | **Written fresh** after the core, guided by bishop-memory, with Bishop words removed |
| Triage and grading (routes, `triage-run.sh`, agents, skills) | large | **Written fresh later**, guided by bishop-memory, as the optional review loop |
| Test suites, mutation harness (`scripts/mutate.py`, `check_mutants.py`) | large | **Guided by** bishop-memory's tests. Mutation testing: phase 2 uses Gremlins (`make mutation`), a Bishop default awaiting the operator's ratification. bishop-memory's harness (`scripts/mutate.py`, `check_mutants.py`) is the reference only |

So roughly 6,000 lines of ingestion code and 1,200 lines of hooks go away, and
the rest is written fresh, guided by bishop-memory, and the tests are written
fresh too, guided by bishop-memory's tests.

## 2. Build order

Phases 0–2 are done. Plan v1 (2026-10-30) replaces the phase list with eight
milestones and a Later set, in this order. Each milestone ends with an exit
check a reviewer can run (§3).

```
M1 Server-mode MCP → M2 Server → M3 Web UI + dashboard → M4 Hooks + CLI
  → M5 Plugin → M6 Import → M7 Pilot → M8 Roll-out → Later
```

This order replaces the phase order adopted as D10 on 2026-10-27 (see §8).
Bishop's crew pack (bishop-harness) is deferred until after roll-out (see
Scope of v1, §3).

## 3. Phases

Phases 0–2 are done and stay here as history. Plan v1 replaces phases 3–7 with
milestones M1, M2, M3, M5 and M6, adds M4, M7 and M8, and moves phases 8–10 to
Later. Each
milestone has work and an exit check a reviewer can run. The milestone table
is under Milestones, below Phase 2.

### Phase 0 — Skeleton

**Work.** Go module `github.com/djm56/cairn-memory`, Go 1.25. `cmd/cairn` with
the sub-command frame and `cairn version`. Makefile (`build`, `test`, `vet`,
`fmt`, `run`). CI on pull requests: `go vet`, `go test -race`, `gofmt` check.
MIT licence (as bishop-memory). `CONTRIBUTING.md` with the rules in §6.
`.gitignore` for `bin/`, `dist/`, `*.db`.

**Exit.** `make test` passes in CI on an empty suite; `bin/cairn version`
prints a version from `git describe`.

### Phase 1 — Contract

**Work.** `internal/core`: the `Service` interface, the request and result
types for every tool in DESIGN.md §4, and the error kinds (not found,
refused by policy, invalid, operator only). `docs/CONTRACT.md`: each tool's
inputs, outputs, errors and an example. `internal/conformance`: a test suite
that takes any `Service` and checks the behaviour, including:

- a whole task: start with a brief and plan, step through it, log events,
  record a review, add a learning, be refused at close with the missing items
  listed, close;
- inserting a step between two others without renumbering;
- refusing every write without a project;
- refusing every operator-only change through the agent interface;
- events are append-only and timestamps are the server's;
- `task_current` returns the one active task, and nothing when none is.

The suite cannot pass yet; it runs against a stub that fails every call.

**Exit.** `CONTRACT.md` covers every tool in DESIGN.md §4. The conformance
suite compiles and reports every case as failing against the stub. Review of
the contract by the operator before phase 2 starts.

### Phase 2 — Core and local store

**Work.** `internal/store`: schema v1 (DESIGN.md §3) as numbered migrations,
WAL mode, busy timeout, FTS5 index and its triggers. Typed query functions
for every table. `internal/core`: the `Service` implementation over the
store, holding every rule: status transitions, the close policy, step
positions, task-id allocation in one transaction, operator-only changes.
`internal/identity`: project resolution from `CAIRN_PROJECT` and
`.cairn.toml` (DESIGN.md §2.1).

Guided by bishop-memory, for reference: `internal/store/sqlite.go` and
`migrate.go`, the FTS setup from `db/schema.sql`, and the allocator's daily
sequence from `internal/api/allocate.go`.

**Exit.** The conformance suite passes against the local `Service`. Two
processes writing the same database at once allocate distinct task ids
(a test). `go test -race` is clean.

### Milestones

Plan v1 (2026-10-30). Milestones M1, M2, M3, M5 and M6 carry the work of
phases 3–7 forward; M4, M7 and M8 are new; phases 8–10 and Postgres sit in
Later. Each exit check is
the only thing that moves the status line (§9).

| Milestone | Was | Work | Exit check |
| --- | --- | --- | --- |
| M1 Server-mode MCP | Phase 3 | `internal/mcp` over `internal/client`; `cairn mcp` refuses to start without a server; tool surface as set in Scope of v1; `cairn init` writes `project` and `server`; `cairn status`, `doctor`, `export task`; compact, paged output; MCP stdio test | A Claude Code project with a hand-written `.mcp.json` completes a small task against `cairn serve` on another machine; `cairn export task` prints it |
| M2 Server | Phase 5 | `cairn serve`: HTTP API over `core`, TLS by files or proxy, loopback default, body caps, `/healthz`; keys with `agent`, `operator`, `viewer` scopes; `cairn key add/list/revoke`; `cairn backup` to a path; systemd unit in `docs/` | Conformance suite passes over HTTP; agent key refused on operator routes; viewer key refused on every write (tests); M1 flow works with `server` set |
| M3 Web UI + dashboard | Phase 6 | Tasks, Learnings (accept, reject, supersede with note), Directives (ratify, scope, `review_by`, retire), Projects (close policy, parallel tasks); Dashboard: running now, waiting on you, quality (30 days), improving (by month), active directive count and oldest age | A lead sees a proposed finding, accepts it, ratifies a directive, and `task_current` returns it next session; a viewer key sees every page and gets 403 on every action |
| M4 Hooks + CLI | New | `cairn hook pre-compact` and `stop`; `cairn enable --agent claude-code` (OpenCode and Pi files embedded but marked experimental); `cairn doctor --agent`; `internal/cli`: `task`, `step`, `event`, `learning`, `directive`, `recall`, `search`, `prime` with `--json`; conformance suite runs through the CLI | On Claude Code a task started before `/compact` continues correctly after it with no user prompt; an agent with only bash completes and closes a task through the CLI |
| M5 Plugin | Phase 4 | `plugins/claude-code/`, skill text with the task loop and the hook reminders, `/cairn:init`, `/cairn:status`, `cairn-launch`, local marketplace, `claude plugin validate` in CI, internal install guide with confirmed tool names | A new developer on a clean machine installs and records a task in 10 minutes from the guide, with no other instruction file |
| M6 Import | Phase 7 | `cairn import bishop-memory --db --dry-run`; mapping per PLAN.md §5.1 minus the reviews table (verdicts become `review` events); idempotent; counts report | Dry run on a copy maps every mission, step, event, finding, directive, criterion, lesson, mistake and deliverable, or names each skip; counts match; history visible on the dashboard |
| M7 Pilot | New | One pilot project, two developers, one operator; daily review of findings; weekly look at the dashboard with a project manager | Two weeks of real tasks; every finding decided within 48 hours; five directives ratified; a project manager can answer 'what is running' from the dashboard alone |
| M8 Roll-out | Cut-over (§5.3) | `.cairn.toml` in every active project; operator keys to leads, viewer keys to project managers; harnesses move between tasks; bishop-memory read-only, then archived | No harness writes to bishop-memory; backups verified by a restore to a scratch server |
| Later | Phases 8–10, Postgres | Scheduled review loop; per-harness plugins; public releases; Postgres store; documentation ingestion | Opened only after M8 |

### Scope of v1

V1 is what the pilot on one project needs, and nothing that only a product
needs.

| In v1 | Cut from v1 | Deferred (after roll-out) |
| --- | --- | --- |
| Tasks: brief, criteria, steps, journal, close policy (as built) | Local no-server mode | Scheduled classify, process, grade (Later) |
| Learnings proposed by agents; accept, reject, supersede by operators | `note_put` / `note_get` (long briefs go in `context`) | Per-harness plugins beyond the CLI (Later) |
| Directives: global, project, agent scope; cap of 12 per call; `review_by` | `review_record` and the reviews table (a `review` event carries verdict and counts until a crew needs more) | Public releases, Homebrew, public marketplace (Later) |
| `recall` and `search` over tasks, learnings, directives | Path-scoped directives | Postgres store, vectors, graph, documentation ingestion |
| HTTP API, API keys with `agent` / `operator` / `viewer` scopes, TLS, body caps, `/healthz` | OKF export | Bishop crew pack (bishop-harness repo) |
| Web UI: Tasks, Learnings, Directives, Projects, Dashboard | Multi-organisation, SSO, hosting | |
| MCP adapter (server mode) and CLI with `--json` | | |
| Hooks: session-start, pre-compact, stop; `cairn enable --agent` | | |
| `cairn init`, `status`, `doctor`, `key`, `backup`, `export task`, `export project` | | |
| Import from bishop-memory | | |

Tool surface after the cuts: `task_current`, `task_start`, `task_get`,
`task_list`, `task_update`, `task_close`, `step_plan`, `step_add`,
`step_update`, `event_log`, `learning_add`, `learning_list`, `directive_list`,
plus `recall` and `search` as read tools. `event_list` folds into `task_get`
with `section: events`. That is 13 tools plus the read tools `recall` and
`search` (15 names).

### After v1

Nothing here is built now, and none of it is a commitment. Three habits during
v1 keep each of these cheap.

- **Postgres store.** All SQL stays in `internal/store`; `Search(query, filters)`
  is one store function; migrations are numbered SQL per backend; timestamps
  stay ISO text. A `store/postgres` package then passes the same conformance
  suite, and `cairn serve --db postgres://…` is the only user-visible change.
- **Vectors and graph.** Both sit behind `Search` and `recall`; the agent tools
  do not change. pgvector for embeddings; recursive queries or a graph extension
  for links between tasks, learnings and directives.
- **Documentation ingestion.** A `documents` table and a `cairn ingest` command.
  Documents are read-only knowledge, kept separate from agent-written learnings
  so the trust boundary stays clear.
- **Scheduled review loop (Later).** Classify, process and grade as
  recommendations only, once the daily human review is routine and the
  dashboard shows the backlog is stable.

## 4. What Bishop needs from Cairn

Bishop's crew pack is bishop-harness's work, not this repository's. Cairn
must provide, by phase 4:

- every operation Bishop's lifecycle uses: brief, plan, steps with
  attempts and assignees, reviews with verdicts, journal, findings, patterns,
  agent notes, directives by agent role, close with lessons and mistakes;
- a close policy strong enough to replace `completion-gate.sh` (DESIGN.md
  §3.7);
- directive scope `agent`, so a rule can target the reviewer role only;
- tool names confirmed live, so the crew's `tools:` lines can name them.

What bishop-harness then does, for the record: drop its `state/`,
`missions/`, `findings/` and `reference/` folders, the state-continuity and
completion-gate hooks, the reconciler wrapper and the connect script; rewrite
its lifecycle doctrine in terms of Cairn's tools; become a plugin that
depends on `cairn`; keep `workspace/` for scratch and QA evidence, and keep
`qa-guard.sh` as a plugin hook scoped to Ripley.

## 5. Migration from bishop-memory

### 5.1 Mapping

| bishop-memory | Cairn |
|---|---|
| `harnesses.name`, `missions.harness` | `projects.name`, `tasks.project` |
| `missions` | `tasks`: id kept as it is; status `not-started`→`open`, `in-progress`→`active`, `blocked`→`blocked`, `complete`+`done`→`done`, `complete`+`failed`→`failed` |
| `documents` of kind `brief` | `tasks.goal` and `context` where they can be found, and the whole brief as a note |
| `documents` of kind `debrief` | a note; the structured parts come from the tables below |
| `mission_steps` | `steps`: `position` from the step number (`7a` sorts after `7`); status mapped |
| `flight_recorder` | `events`: kind from the event word (`step-sync`→`finished`, `blocked`→`blocked`, others→`note` with the word kept in `data`) |
| `mission_criteria` | `task_criteria` |
| `mission_lessons` | `task_lessons` (`similar-mission`→`similar-task`, `reuse-step`→`reuse`) |
| `agent_mistakes` | `task_mistakes` |
| `mission_deliverables` | `task_files` with role `deliverable` |
| `findings` | `learnings` kind `finding`; `approved`→`accepted`, other statuses kept |
| `patterns` | `learnings` kind `pattern` |
| `service_records` | `learnings` kind `agent-note` |
| `directives` | `directives`, scope `global` unless the source names a harness |
| review verdicts, parsed at import from reviewer steps' notes and summaries with bishop-memory's `internal/review` rules (it stores no review table) | `reviews` |
| `mission_grades`, triage tables | carried in phase 8, when those tables exist |
| `crew` | not carried; agent names live on rows |
| workspace documents | not carried; scratch stays where it is |

### 5.2 Coexistence

bishop-memory and the live harnesses keep running unchanged while Cairn is
built. Nothing in this plan writes to bishop-memory's database or to any
harness. The import reads a copy.

### 5.3 Cut-over

The cut-over is milestone M8 (Roll-out). Before it:

1. A new test project uses Cairn from M1.
2. After M6, the history is imported into a Cairn server, and the import is
   repeated (it is idempotent) until cut-over.

In M8:

3. Each harness moves when its crew pack or instructions are ready, between
   missions, never during one. bishop-memory stays read-only for that harness
   afterwards.
4. bishop-memory's nightly classify, process and grade runs are switched off.
   Not before: they must not break while any harness still writes to
   bishop-memory.
5. bishop-memory is archived when no harness writes to it.

Rollback at any step: the harness goes back to its bishop-memory
configuration. Tasks recorded in Cairn in the meantime can be exported as
Markdown with `cairn export`.

## 6. Engineering rules

From what went well and badly in bishop-memory:

- **Rules live in `core`.** A rule enforced in a handler, a hook or a doctrine
  file is a rule that a second path will miss. The conformance suite is the
  gate.
- **No SQL in handlers.** bishop-memory's handlers carry inline SQL, and its
  typed store was left as a placeholder. Cairn starts with the store typed.
- **Every tool result is read by a model.** Keep it small, paged and
  readable. Test the size of `task_current` on a large task.
- **Tests before code for each tool**, through the conformance suite.
- **Mutation testing**: phase 2 uses Gremlins (`make mutation`) on `core` and
  `store`. A Bishop default, awaiting the operator's ratification; bishop-memory's
  harness was the reference only.
- **No silent defaults for identity.** A missing project is an error that
  says how to fix it.
- **Docs are dated.** Status lines carry a date, and claims carry evidence,
  as in bishop-memory's plans.

## 7. Risks

- **Rebuilding more than needed.** Version 1 is phases 0–4: a local memory a
  plain Claude Code project can use. The server, the UI, the import and the
  review loop follow. Anything not in DESIGN.md §4 waits for a reason.
- **Losing hard-won behaviour.** bishop-memory's tests encode bugs already
  found: the mission-id extraction rule, the unknown-status warning, the
  N/A grade, the FK guards in grade rebuilds. Phase 7 and phase 8 go through
  those tests and carry each one that still applies.
- **Two systems at once.** Until cut-over the operator watches two services.
  Keep the overlap short by moving harnesses soon after phase 7.
- **Plugin facts change.** Tool naming for plugin servers and the subagent
  wildcard form are inferred, not documented. Phase 4 confirms them live.
  Re-read the Claude Code plugin pages before phases 4 and 10.
- **Local mode under concurrency.** Several MCP processes on one SQLite file
  is safe in WAL mode, but busy errors must surface as a retry, not a lost
  write. Phase 2 tests it.

## 8. Decisions

**Adopted 2026-10-27 by the operator: D1–D10, each as recommended.** D10's
order was the phase order that §2 and §3 then gave. Plan v1 (2026-10-30)
replaces that order with the milestones in §2 and §3. D11–D18 come from Plan
v1's decisions table.

- **D1. Vocabulary: "task".** Bishop says mission; Cairn says task, which
  every harness understands. Bishop's doctrine can keep its word.
  *Recommended.*
- **D2. Local mode with no daemon.** `cairn mcp` opens a local SQLite file
  when no server is configured. The alternative is always running
  `cairn serve`. *Recommended: local mode, because it makes the first install
  one step.* **Superseded by D11** (Plan v1, 2026-10-30); kept as the record.
- **D3. One `learnings` table** for findings, patterns and agent notes, with
  a `kind`. The alternative is three tables, as in bishop-memory.
  *Recommended: one table; they share a lifecycle and the review page.*
- **D4. Task ids `task-YYYYMMDD-NN`**, a global daily sequence as in
  bishop-memory, so ids stay unique across projects and imported mission ids
  keep their own form. *Recommended.*
- **D5. Step positions, not step numbers.** Display numbers are computed.
  *Recommended.*
- **D6. One binary `cairn`** with sub-commands, rather than separate server
  and adapter binaries. *Recommended.*
- **D7. The project file is `.cairn.toml`** at the project root.
  *Recommended.*
- **D8. The review loop waits for phase 8.** Version 1 has no scheduled
  agents. *Recommended.* **Superseded by D17** (Plan v1, 2026-10-30); kept as
  the record.
- **D9. Local marketplace first**, public after a review of the plugin's
  hook text and install guide. *Recommended.*
- **D10. Build from the outside in**, in the order in §2: contract, core,
  MCP, plugin, server, UI, migration, review loop. *Recommended; confirmed
  by the operator 2026-10-27.* **Superseded by the M1–M8 order in §2**
  (Plan v1, 2026-10-30); kept as the record.

Plan v1 (2026-10-30) adds D11–D18. Each row names what it replaces and why.
D2, D8 and D10 are marked superseded above; the other rows replace something
that is not a numbered decision here, and the table says what.

**Adopted 2026-10-30 by the operator (Plan v1): D11–D18.**

- **D11. Central server only.** `cairn mcp` and the CLI require a `server` in
  `.cairn.toml` or `CAIRN_SERVER`; no local database path for clients.
  *Replaces D2 (local mode). Why: one place to look and back up; one backend to
  test; the first users are a team, not one developer.*
- **D12. SQLite stays, behind `Service` and `store`.** Postgres is a second
  `store` package later, gated by the conformance suite. *Replaces: nothing.
  Why: already built and passing; vectors and graph are not v1.*
- **D13. Three key scopes:** `agent` (tools), `operator` (decisions,
  ratification), `viewer` (read-only UI and dashboard). *Replaces DESIGN.md §8's
  two agent and operator scopes plus admin. Why: leads and project managers
  need to look without being able to change anything.*
- **D14. Two agent surfaces.** MCP for Claude Code; a CLI over the same HTTP API
  for every other harness. *Replaces: nothing. Why: no per-harness plugin work
  in v1; bash is enough.*
- **D15. Hooks on `session-start`, `pre-compact` and `stop`,** installed by
  `cairn enable --agent`. *Replaces DESIGN.md §4.1, session-start only. Why: a
  task loop must survive compaction.*
- **D16. Directives returned per call are capped at 12,** ranked agent >
  project > global, then newest. Each has a `review_by` date, 90 days by
  default. *Replaces: nothing. Why: instruction-following degrades as rules pile
  up; a cap forces retirement.*
- **D17. No scheduled review agents in v1.** The review loop moves to Later; a
  lead reviews proposed findings daily from the UI. *Replaces D8 (already
  deferred). Why: token cost and oversight; the UI is the loop.*
- **D18. Repo, binary and plugin are named `cairn`.** The category line is
  "operating record for coding agents". *Replaces "cairn-memory". Why: "memory"
  invites the wrong comparison and hides task state and directives.*

## 9. Project management

- **Milestones** mirror the phases in §3, one milestone per phase.
- **`.github/roadmap.json`** holds copies of each phase's exit check and work
  items from §3. Change §3 and roadmap.json together.
- **Board, labels and bootstrap.** The labels, the Projects board and the
  roadmap issues are set up by `scripts/github/bootstrap.sh`. It is a dry run
  by default. `--apply` fills what is missing and never changes what a person
  set. The operator runs `--apply`.
- **Wiki.** The GitHub wiki is published from `wiki/` in this repository.
- **Status.** The status line appears in README.md, in this file, in
  docs/DESIGN.md, in docs/CONTRACT.md (whose line states the contract's own
  state, draft or approved, rather than the phase), and at the top of each
  `wiki/*.md` content page. The `cairn-phase` marker appears in README.md and in
  this file. All of these carry the phase only: which phase is current, and
  whether its exit check has passed. They change together, and only when a
  phase's exit check passes, so they cannot go stale between exits.
  `scripts/check-docs.sh` checks only the README and PLAN markers, not the
  status lines. They never carry pull request numbers or merge state. Live
  progress (pull requests, issues, merges) is on the
  [milestones](https://github.com/djm56/cairn-memory/milestones) and the
  [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).

## 10. Open questions

- [ ] Pilot project and the two developers (M7).
- [ ] A second operator besides the first, so that daily review does not
  depend on one person.
- [ ] Does the proxy in front of the server terminate TLS, or does `cairn serve`
  carry certificates?
- [ ] Do viewers see proposed learnings, or only ratified directives?
- [ ] Keep "Cairn" as the only name, or give the tool a separate internal name?
- [ ] Rename to `cairn` now (one PR, before M1) or after M8?
- [ ] M1's exit check runs against `cairn serve`, which M2 builds. Does M1 move
  `internal/client` and a serve skeleton into M1, or does it test against an
  in-process test server?
