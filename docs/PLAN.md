# Cairn Memory — Build Plan

<!-- cairn-phase: 1 -->

**Status as of 2026-10-28: phase 1 (contract) is built and waits on the
operator's review of [CONTRACT.md](CONTRACT.md) §9. Live progress is on the
[milestones](https://github.com/djm56/cairn-memory/milestones) and the
[Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).
The rule for this line is in §9.**

What Cairn is and how it works is in [DESIGN.md](DESIGN.md).

## 1. Where this comes from

Cairn replaces [bishop-memory](https://github.com/djm56/bishop-memory) (local
checkout `/Users/djm56/bishop-memory`). bishop-memory works, and it holds the
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

Removing those edges from bishop-memory while it stays compatible with the
live Bishop harnesses would cost more than building the core again, cleanly,
and porting the parts that are already good. That is this plan.

### 1.1 What carries over, and what does not

From bishop-memory, by size (non-test lines at `4c170b6`):

| bishop-memory | Lines | In Cairn |
|---|---|---|
| `internal/importer` (Markdown → rows) | 1,863 | **Dropped.** Agents write through tools |
| `internal/debrief`, `criteria`, `review` (heading and verdict parsers) | 1,016 | **Dropped.** The same data arrives as tool fields; the parsers' test cases become validation cases |
| `scripts/reconcile-memory.py`, `export-decisions.py`, `push-memory.py` | ~2,300 | **Dropped** |
| Harness hooks and connect script (bishop-harness) | ~1,200 | **Dropped** |
| `internal/store` (SQLite, migrations, FTS) | 929 | **Ported**, with typed queries for every table |
| `internal/api` (HTTP) | 7,726 | **Ported in part.** Handlers become thin over `core`; inline SQL moves to `store` |
| `cmd/mcpd` (MCP adapter) | 2,090 | **Rewritten** over the `Service` interface; the HTTP-error and output-shaping helpers are kept |
| `internal/auth`, `middleware`, `config` | 588 | **Ported** |
| `internal/ui` (HUD, triage, crew, performance pages) | 92 + HTML | **Ported** after the core, with Bishop words removed |
| Triage and grading (routes, `triage-run.sh`, agents, skills) | large | **Ported later**, as the optional review loop |
| Test suites, mutation harness (`scripts/mutate.py`, `check_mutants.py`) | large | **Ported as practice**: the mutation harness is reused against Cairn's tests |

So roughly 6,000 lines of ingestion code and 1,200 lines of hooks go away,
and the rest is ported with its tests.

## 2. Build order

The plan builds **from the outside in**: first the contract an agent sees
(the tools and their behaviour, as a test suite), then the core that
satisfies it, then the ways to reach it (MCP, plugin, server), then the
operator's pages, then migration from bishop-memory, then the review loop.
Each phase leaves something that works end to end. *(Decision D10 confirms
this order.)*

```
0 skeleton → 1 contract → 2 core + local store → 3 MCP → 4 Claude Code plugin
          → 5 server + remote → 6 web UI → 7 migration → 8 review loop
          → 9 other clients → 10 releases
```

Bishop's crew pack (bishop-harness) can start after phase 4.

## 3. Phases

Each phase has work and an exit check a reviewer can run.

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

Port from bishop-memory: `internal/store/sqlite.go` and `migrate.go`, the FTS
setup from `db/schema.sql`, and the allocator's daily sequence from
`internal/api/allocate.go`.

**Exit.** The conformance suite passes against the local `Service`. Two
processes writing the same database at once allocate distinct task ids
(a test). `go test -race` is clean.

### Phase 3 — MCP adapter

**Work.** `internal/mcp`: every tool in DESIGN.md §4 over `Service`, with
compact output and paging. `cairn mcp` (local backend). `cairn init`,
`cairn status`, `cairn doctor`, `cairn hook session-start`, `cairn export`.

Port from bishop-memory: the result and error helpers in `cmd/mcpd/main.go`
(HTTP-status mapping, malformed-body guard), and the tool-description style.

**Exit.** A plain Claude Code project with a hand-written `.mcp.json` entry
for `cairn mcp`, after `cairn init`, completes a small real task: Claude
starts it, plans it, steps through it, records a learning and closes it.
`cairn export` prints the task as Markdown. An MCP-level test drives the same
flow through stdio.

### Phase 4 — Claude Code plugin

**Work.** `plugins/claude-code/` as in DESIGN.md §6.1. `.claude-plugin/
marketplace.json` at the repository root (local marketplace first). The
`cairn` skill: the task loop, when to log, what makes a good finding, how to
answer a close refusal. `cairn-launch`: finds `cairn` on PATH, in
`~/.local/bin`, or in `CAIRN_HOME/bin`; otherwise prints the install command.
`claude plugin validate` in CI. An install guide.

Confirm in a live session, and record in the guide: the exact tool names
(`mcp__plugin_cairn_cairn__*` is inferred from the docs, not stated), that
`CLAUDE_PROJECT_DIR` reaches `cairn mcp`, and the SessionStart output.

**Exit.** On a machine with no bishop-memory: install `cairn`, add the
marketplace, install the plugin, run `/cairn:init` in an empty project, and
ask Claude to do a small task "using Cairn". The task is recorded with its
brief, steps, events and close, without any other instruction file.

### Phase 5 — Server and remote backend

**Work.** `cairn serve`: the HTTP API over `core`, API keys with scopes
(DESIGN.md §8), TLS, loopback by default, body caps, `/healthz`.
`internal/client`: the `Service` over HTTP. `cairn mcp` uses it when
`server` is set. `cairn key add/list/revoke`.

Port from bishop-memory: `internal/auth/keys.go`, `internal/middleware`,
`internal/config`, the TLS and key handling in `cmd/memoryd`, and the network
tests (`internal/api/network_test.go`).

**Exit.** The conformance suite passes against the HTTP client talking to a
test server. An agent key is refused on an operator route (a test). The
phase 4 flow works with `server` set in `.cairn.toml`.

### Phase 6 — Web UI

**Work.** Pages embedded in the binary, served by `cairn serve`:

- **Tasks**: a list across projects; one task's page with its brief, steps,
  journal, reviews and close record. Ported from bishop-memory's mission HUD.
- **Learnings**: proposed findings with accept, reject and supersede, each
  with a note. Ported from the triage review page.
- **Directives**: ratify a directive from an accepted finding, edit scope,
  retire.
- **Projects**: settings, including the close policy.

**Exit.** The operator can run the whole learning loop from the browser: see
a proposed finding, accept it, ratify it as a directive, and see it returned
by `directive_list` and `task_current` in the next session.

### Phase 7 — Migration from bishop-memory

**Work.** `cairn import bishop-memory --db <path> [--dry-run]`. Reads the
bishop-memory SQLite file directly; never reads harness Markdown. The mapping
is in §5. Idempotent: running it twice changes nothing the second time.
Reports counts for each table, read and written, and every row it could not
map.

**Exit.** A dry run against a copy of the live bishop-memory database maps
every mission, step, event, finding, directive, criterion, lesson, mistake,
deliverable and grade, or names each one it skipped and why. Counts are
checked against the source. The operator reviews the result in the web UI.

### Phase 8 — Review loop

**Work.** Port triage and grading (DESIGN.md §7): categories,
classification, groups, recommendations, directive proposals, grades, runs;
the MCP review profile; the agent definitions and skills, rewritten without
Bishop names; an engine-neutral runner (`cairn review run classify|process|
grade`) that starts the chosen client headless. A scheduling guide for
launchd and systemd.

Port from bishop-memory: the triage and grade routes in
`internal/api/triage.go` and `grades.go`, `internal/store/triage_runs.go` and
`mission_grades.go`, `scripts/triage-run.sh`, the three agents and two skills
under `.claude/`, and their tests.

**Exit.** A scheduled run classifies new findings and grades closed tasks,
and the operator accepts the recommendations on the review page. Then the
bishop-memory nightly runs are switched off (§5.3).

### Phase 9 — Other clients

**Work.** OpenCode: a generated `opencode.json` entry and instruction file;
check the timeout and the tool prefix live. pi: `.pi/mcp.json` with
`"exposure": "direct"`; check that the tools are visible to the model. A
short page per client.

**Exit.** Each client completes the phase 3 flow in a live session.

### Phase 10 — Releases

**Work.** Release archives for linux and darwin, amd64 and arm64, built by CI
on a tag (`CGO_ENABLED=0`, as bishop-memory's `make dist`). A Homebrew tap. A
plugin release tagged `cairn--v<version>`, which is the tag form plugin
dependencies resolve against. A public marketplace only after a review of the
plugin's hook text *(decision D9)*.

**Exit.** A clean machine installs `cairn` from a release, installs the
plugin, and completes the phase 4 flow.

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

1. A new test project uses Cairn from phase 4.
2. After phase 7, the history is imported into a Cairn server, and the import
   is repeated (it is idempotent) until cut-over.
3. Each harness moves when its crew pack or instructions are ready, between
   missions, never during one. bishop-memory stays read-only for that harness
   afterwards.
4. After phase 8 runs cleanly for a week, bishop-memory's nightly classify,
   process and grade runs are switched off. Not before: they must not break
   while any harness still writes to bishop-memory.
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
- **Mutation testing** with the harness ported from bishop-memory, on `core`
  and `store`, from phase 2.
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
order is the one in §2.

- **D1. Vocabulary: "task".** Bishop says mission; Cairn says task, which
  every harness understands. Bishop's doctrine can keep its word.
  *Recommended.*
- **D2. Local mode with no daemon.** `cairn mcp` opens a local SQLite file
  when no server is configured. The alternative is always running
  `cairn serve`. *Recommended: local mode, because it makes the first install
  one step.*
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
  agents. *Recommended.*
- **D9. Local marketplace first**, public after a review of the plugin's
  hook text and install guide. *Recommended.*
- **D10. Build from the outside in**, in the order in §2: contract, core,
  MCP, plugin, server, UI, migration, review loop. *Recommended; confirmed
  by the operator 2026-10-27.*

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
  docs/DESIGN.md, and at the top of each `wiki/*.md` content page. The
  `cairn-phase` marker appears in README.md and in this file. All of these
  carry the phase only: which phase is current, and whether its exit check has
  passed. They change together, and only when a phase's exit check passes, so
  they cannot go stale between exits. `scripts/check-docs.sh` checks only the
  README and PLAN markers, not the status lines. They never carry pull request numbers or merge
  state. Live progress (pull requests, issues, merges) is on the
  [milestones](https://github.com/djm56/cairn-memory/milestones) and the
  [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).
