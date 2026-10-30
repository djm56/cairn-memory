# Cairn Memory — Design

**Status as of 2026-10-30: M1 (Server-mode MCP) is current. Phases 0 to 2 are
done, and their exit checks passed. Live progress is on the
[milestones](https://github.com/djm56/cairn-memory/milestones) and the
[Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).**

The build order is in [PLAN.md](PLAN.md). The decisions in
PLAN.md §8 were adopted on 2026-10-27; this document marks each place that
rests on one as *(decision Dn)*.

## 1. What Cairn is

Cairn is an operating record for coding agents. An agent records what it is working on,
the steps it takes, what happened, and what it learned. The next agent, in
the same project or another one, reads that back before it starts.

A cairn is a pile of stones left on a trail for whoever comes next. Each task
in Cairn leaves markers of the same kind: what worked, what went wrong, and
rules that the next agent must follow.

Cairn has four jobs:

1. **Task state.** The current task, its plan, its steps, and a journal of
   events. An agent that starts a new session picks up where the last one
   stopped.
2. **Learning.** Agents record findings (things to change), patterns (things
   that worked) and notes about agents. An operator accepts or rejects each
   finding. Accepted findings become **directives**: rules that agents read
   before they work.
3. **Recall.** Before a task starts, Cairn returns the lessons, directives and
   similar past tasks that apply to it.
4. **Review.** A web page where the operator follows tasks, decides on
   findings and ratifies directives. Optional scheduled agents classify
   findings and grade finished tasks (PLAN.md, Later).

### 1.1 Principles

- **Any harness.** Cairn assumes no agent framework. Plain Claude Code is the
  first client. Bishop, OpenCode, pi and others are clients of the same kind.
  Nothing in the core names a particular crew, agent or file layout.
- **The database is the source of truth.** Agents write through tools and
  read through tools. There are no Markdown files to keep in step, so there
  is no importer, mirror or reconciler.
- **Structured, not parsed.** Lessons, mistakes, acceptance criteria and
  review verdicts are tool fields. Nothing is parsed out of Markdown headings
  afterwards.
- **One binary.** `cairn` is the server, the MCP adapter, the hook helper and
  the admin tool.
- **One server.** Every client calls one `cairn serve` over HTTP, so a single
  team or several machines share one place to look and back up *(decision
  D11)*. `cairn mcp` and the CLI require `server` in `.cairn.toml` or
  `CAIRN_SERVER`. The local database path that D2 designed (the MCP adapter
  opening a database when no server is set) was cut by D11 before it was built.
  M1 builds the server-only adapter *(decision D2, superseded by D11)*, and the
  `internal/api` handlers with no network listener in the shipped binary (decision D19).
- **The operator decides.** Agents propose findings and directives. Only the
  operator accepts them. No tool lets an agent change a finding's status or
  ratify a rule.
- **Small, compact tool output.** Every tool result is written to be read by
  a model with a limited context. Lists are short by default and paged.

### 1.2 What Cairn is not

- Not an agent framework. It does not decide what agents do, only what they
  remember.
- Not a vector database or a general knowledge base. Search is full-text
  (SQLite FTS5). Embeddings may come later, behind the same `search` tool.
- Not a place for project files. Scratch work, review drafts and test
  evidence stay in the project's own folders.

## 2. Concepts

| Concept | Meaning |
|---|---|
| **Project** | A codebase that uses Cairn. The unit of identity: every task, event and learning belongs to one. Named in the project's `.cairn.toml`. |
| **Client** | The program connecting: `claude-code`, `opencode`, `pi`, or another. Recorded on each session, never used as identity. |
| **Agent** | The name of whoever made a call: `main`, or a sub-agent's name (`reviewer`, `hicks`). Chosen by the client. |
| **Session** | One run of a client, when the client supplies an id (Claude Code hooks do). Optional. |
| **Task** | A unit of work with a goal, a brief, a plan and an outcome. Bishop calls it a mission *(decision D1)*. |
| **Step** | One planned piece of a task, assigned to an agent, with attempts and a status. |
| **Event** | A row in the task's journal: something that happened, by whom, when. |
| **Review** | A verdict on a step: approved or not, with counts of critical issues and warnings. |
| **Learning** | Something worth keeping: a finding, a pattern or an agent note *(decision D3)*. |
| **Directive** | A rule agents must follow, ratified by the operator, with a scope. |
| **Note** | Free Markdown attached to a task (design notes, a long brief), searchable. |

### 2.1 Identity

Every agent call carries a project. Every write also carries an agent. The
stored actor is `<project>:<agent>`.

The MCP adapter finds the project in this order:

1. `CAIRN_PROJECT` in its environment.
2. `project = "<name>"` in `.cairn.toml`, found by walking up from
   `CLAUDE_PROJECT_DIR` (set by Claude Code for MCP servers) or from the
   working directory.
3. Otherwise every call is refused with an error that names `cairn init`.
   There is no default project. A silent default writes under the wrong
   name, which is the trap bishop-memory's `claude-code` default fell into.

A `.cairn.toml` that is a directory is refused, not skipped: the walk stops at
it and the call is `invalid`. The error names that path, as in
`<path> is a directory, not a project file`. It does not name `cairn init`,
because the fix is to move the directory aside, and init would not replace it.

`.cairn.toml` is committed to the project, so everyone who works on it writes
under the same name. Its full contents:

```toml
project = "acme-api"          # required; lowercase letters, digits, hyphens
server = "https://cairn.lan:8787"   # required from M1 (decision D11); D2's local database was never built
```

The API key never goes in this file. It comes from the plugin's secure option,
`CAIRN_API_KEY`, or `~/.config/cairn/client.env`.

The server is resolved the same way, from `CAIRN_SERVER` or else the `server`
key of the nearest `.cairn.toml`, found by the same walk (M1). `CAIRN_SERVER`
is read first, and an empty value counts as unset. A call with neither is
refused with an error that names `cairn init`, and there is no default server.
`cairn mcp` refuses to start without one.

## 3. Data model

SQLite through `modernc.org/sqlite` (pure Go, no C toolchain). Every table
carries `project`. Times are UTC, ISO 8601, set by the server. Schema changes
are numbered migrations, run at start-up. Each migration is an embedded SQL
file, applied in its own transaction and recorded in `schema_migrations`. The
driver opens with `_txlock=immediate`, so the version is read under the write
lock. Enumerated columns carry CHECK constraints, and references are declared
foreign keys. `tasks.project` is immutable: a trigger in migration 0002 refuses a
change. `events.project` must equal its task's project: a trigger in migration
0003 refuses an event that names another project. Core owns two rules the schema
does not state: a directive's scope must match its project and agent, and a
review's `critical` and `warnings` counts are not negative. The store reads
outside transactions, reads one snapshot inside `WithReadTx` when several reads
must agree, and writes inside `WithTx`.

### 3.1 Tasks

```
tasks
  id            TEXT PK        task-20261027-01  (decision D4)
  project       TEXT NOT NULL
  title         TEXT NOT NULL
  goal          TEXT NOT NULL  one or two sentences: what done looks like
  context       TEXT           why now, background, links
  constraints   TEXT           what must not change
  status        TEXT NOT NULL  open | active | blocked | done | failed | abandoned
  priority      TEXT NOT NULL  low | normal | high | urgent
  owner         TEXT           agent leading the task
  next_action   TEXT
  blockers      TEXT
  outcome_summary TEXT         set at close
  no_lessons_reason TEXT       why task_lessons is empty, set at close
  opened_at, started_at, closed_at, updated_at

task_criteria          acceptance criteria, set in the brief, judged at close
  task_id, ord, criterion, result (met | partial | unmet | unclear), evidence

task_files             key files the brief names, and deliverables at close
  task_id, ord, path, role (key | deliverable), note
  (primary key task_id, role, ord: key and deliverable rows share the table)

task_lessons           written at close
  task_id, ord, kind (wrong-assumption | reuse | similar-task), text, why, correction

task_mistakes          written at close
  task_id, ord, agent, mistake, impact (low | med | high), correction, prevention
```

A task is `active` for at most one per project at a time unless the operator
allows parallel tasks in the project settings. `task_current` returns it.

### 3.2 Steps

```
steps
  id         INTEGER PK
  task_id    TEXT NOT NULL
  position   REAL NOT NULL     ordering key; display numbers are computed
  title      TEXT NOT NULL
  agent      TEXT              who is assigned
  status     TEXT NOT NULL     pending | active | done | failed | skipped
  attempts   INTEGER NOT NULL DEFAULT 0
  summary    TEXT              the latest result
  started_at, ended_at
```

**No renumbering.** Inserting a step between 7 and 8 gives it a position
between theirs. The step shown as "8" afterwards is computed from the order.
Tools accept either a step id or its displayed number. This removes the
"later steps renumbered +2" bookkeeping that Markdown plans needed.

### 3.3 Events and reviews

```
events
  id, task_id, project, step_id (nullable), agent, session_id (nullable),
  kind TEXT NOT NULL    note | started | finished | blocked | unblocked |
                        handoff | decision | review | close | custom
  text TEXT NOT NULL,
  data TEXT             optional JSON for the kind
  occurred_at           server time; a client may not set it

reviews
  id, task_id, step_id, reviewer, verdict (approved | not-approved),
  critical INTEGER, warnings INTEGER, summary, created_at
```

Events are append-only. There is no update or delete tool.

**Cut from the v1 agent surface.** The `reviews` table is built in the store
today. M1's tool consolidation removes `review_record` from the MCP surface,
and the `review` event carries the verdict, `critical` and `warnings` in
`data` instead (Scope of v1, PLAN.md §3). From M1 no MCP tool writes or reads it.

### 3.4 Learnings and directives

```
learnings
  id, project, task_id (nullable), agent,
  kind     TEXT NOT NULL   finding | pattern | agent-note        (decision D3)
  title    TEXT NOT NULL
  body     TEXT NOT NULL   the suggestion, the solution, or the note
  target   TEXT            what it would change: a file, a rule, an agent
  rationale TEXT
  example  TEXT
  status   TEXT NOT NULL   proposed | accepted | rejected | applied | retired | superseded
  superseded_by INTEGER
  decided_by, decided_at, decision_note
  created_at

directives
  id, code TEXT UNIQUE      D-0001
  scope    TEXT NOT NULL    global | project | agent
  project  TEXT             when scope is project
  agent    TEXT             when scope is agent (a role name)
  title, applies_when, rule, rationale, reviewer_check, example
  status   TEXT NOT NULL    active | retired
  source_learning INTEGER   the finding it came from, if any
  review_by TEXT            planned: the date the rule is due for reaffirming or retiring;
                            90 days after ratification by default (decision D16). The column
                            lands with the directive cap in M2; the UI edits it in M3.
  ratified_by, ratified_at, retired_at
```

Agents create learnings with status `proposed` only. Status changes and
directive ratification are operator routes, used from the web page or the
CLI, never MCP tools.

Global directives let one lesson apply to every project, which is how a
learning in one codebase reaches the next.

Planned (decision D16, M2): each directive has a `review_by` date, 90 days from
ratification by default. A directive past its date is left out of the capped
set and flagged on the UI.

### 3.5 Notes and search

```
notes
  id, task_id (nullable), project, title, body (Markdown), created_by, updated_at

search_index   FTS5 over tasks (title, goal, context, outcome), events (text),
               learnings (title, body, rationale), directives (rule, title),
               notes (title, body), lessons and mistakes
```

`search_index` is one FTS5 table, with `kind`, `ref`, `project` and `task_id` as unindexed
columns. Each source table has insert, update and delete triggers. Update and
delete find a row by `ref`, which scans the index; that is accepted at v1 scale.

**Cut from the v1 agent surface.** The `notes` table is built in the store
today. M1's tool consolidation removes `note_put` and `note_get` from the MCP
surface, and a long brief goes in the task's `context` (Scope of v1, PLAN.md
§3). No v1 tool writes or reads notes.

### 3.6 Projects and settings

```
projects
  name PK, display_name, created_at, last_seen_at,
  settings TEXT    JSON: close policy, parallel tasks
sessions
  id PK, project, client, agent, started_at, last_seen_at
api_keys        (server mode) name, hash, scopes, created_at, last_used_at
  (surrogate key `id`; this table names no key of its own)
```

The step-review rule is deferred to Later (#41).

### 3.7 The close policy

`task_close` is refused until the task meets its project's close policy. The
default policy:

- an outcome (`done` or `failed`) and a summary;
- a result for every acceptance criterion;
- at least one lesson, or `lessons: []` with `no_lessons_reason` set;
- the mistakes list present (it may be empty);
- no step left `active`.

The policy is project settings, changed by the operator, never by an agent.
This replaces bishop-harness's `completion-gate.sh`, and it checks content,
not the shape of a Markdown table.

## 4. Tool surface (MCP)

Each tool's fields, errors and rules are in [CONTRACT.md](CONTRACT.md).
Tool names are short and unprefixed; the client adds the server prefix
(Claude Code: `mcp__plugin_cairn_cairn__task_start`). Each write tool takes
`agent`. Output is compact JSON or short text, designed to be read by a model.

Plan v1 (decisions D11 and D14) sets the agent surface for M1: 13 tools plus
the read tools `recall` and `search` (15 names). The table below lists all 15.
Until M1, the contract and `internal/core` still carried `event_list`,
`review_record`, `note_put` and `note_get`, and no MCP tool wrapped them. M1
removes them, and `event_list` folds into `task_get` (`section: events`). Each
tool also has a CLI command over the same HTTP API (`internal/cli`, M4), which
is how every harness other than Claude Code reaches Cairn.

In M1, `internal/mcp` registers the fifteen tools of the table below, each one
call of the service. Each write tool takes `agent`, which is the caller's agent
(CONTRACT §1.1). The two step tools that also name a step's assignee take it as
`assignee` instead, because there `agent` is the caller: `step_add` and
`step_update` (CONTRACT §3 names the field `agent` for both, and the MCP surface
differs from it only there).

| Tool | Purpose |
|---|---|
| `task_current` | The active task: brief, steps with display numbers, the last few events, and the directives that apply. The first call of a session. Planned: directives capped at 12, ranked (D16). |
| `task_start` | Create a task with its brief (title, goal, context, constraints, criteria, key files) and, optionally, its plan. Returns the id. |
| `task_get` | One task in full, or one section of it (`brief`, `steps`, `events`, `close`). `section: events` is the journal, paged; it replaces `event_list` in M1. |
| `task_list` | Recent tasks in this project, filtered by status. |
| `task_update` | Status, priority, owner, next action, blockers; edit the brief before work starts. |
| `task_close` | Outcome, summary, criteria results, lessons, mistakes, deliverables. Refused with a list of what is missing when the close policy is not met. |
| `step_plan` | Replace the task's pending steps with a list. Steps already started are kept. A pending step that has an event or a review is not removed: the call is refused, and the agent skips that step first (CONTRACT §3). |
| `step_add` | Insert one step after a given step. |
| `step_update` | Start, finish, fail or skip a step; record an attempt and a summary. Also writes the matching event. |
| `event_log` | Append a journal event. |
| `learning_add` | Propose a finding, pattern or agent note. |
| `learning_list` | Learnings by status, kind, project or target. |
| `directive_list` | Directives that apply to this project, optionally to one agent role. Planned: capped at 12, ranked (D16). |
| `recall` | Given a goal, return the directives, lessons, mistakes and similar past tasks that apply, across projects. Planned: directives capped at 12, ranked (D16). |
| `search` | Full-text search across everything above. |

Tools that do not exist on purpose: anything that changes a learning's
status, ratifies or retires a directive, deletes a task, or edits an event.

### 4.1 Session start

`cairn hook session-start` prints, for a SessionStart hook, a short block of
context: the active task (title, next action, current step), the count of
directives that apply, and one line telling the agent to call `task_current`.
When no task is active it says so in one line. It writes no task, step, event or
learning. Its one call is a read, and the first call of a project creates the
project's row on the server, as every first call does (CONTRACT §9 item 38). With
no project, no server, or a server that does not answer in 5 seconds, it prints
nothing on stdout, says why on stderr, and exits 0, so the session starts.

Plan v1 (decision D15) sets three hooks: `session-start`, `pre-compact` and
`stop`. `session-start` is built in M1 (issue #16). `pre-compact` and `stop` are
planned for M4.
`cairn enable --agent` installs all three.

- `pre-compact` (planned, M4) prints the active task, the current step, the
  next action and the directive count, so a task loop survives compaction.
- `stop` (planned, M4) prints a one-line warning when a step is active without
  a summary.

## 5. Architecture

The target after M2: one server, no local backend. D2 designed a local backend,
and D11 cut it before it was built; §5.3, "The server", says what replaces it.
M1 builds the server-only adapter and the `internal/api` handlers. It builds no
network listener in the shipped binary and no `cairn serve`: `cairn serve` is M2, and M1's tests run
against an in-process server (D19).

```
            ┌──────────── project ─────────────┐
            │ client (Claude Code, OpenCode…)  │
            │ cairn plugin: MCP entry, skill,  │
            │   hooks                          │
            │ .cairn.toml                      │
            └───────────────┬──────────────────┘
                            │ MCP stdio (the CLI from M4)
                    ┌───────▼────────┐
                    │   cairn mcp    │
                    └───────┬────────┘
                            │ HTTP + API key
                    ┌───────▼──────────┐
                    │   cairn serve    │
                    │ HTTP API, web UI │
                    └───────┬──────────┘
                    ┌───────▼────────────┐
                    │  core service      │  validation, policy, identity
                    ├────────────────────┤
                    │ store (SQLite)     │
                    └────────────────────┘
```

### 5.1 Packages

| Package | Job |
|---|---|
| `internal/core` | The `Service` interface and its implementation: every rule (close policy, status transitions, operator-only changes) lives here and nowhere else. |
| `internal/store` | SQLite access, migrations, FTS. Typed queries; no SQL in handlers. |
| `internal/api` | HTTP handlers. Thin: decode, call `core`, encode. Built in M1: handlers only, in the form of §5.4. The shipped binary has no network listener (D19). The `cairn serve` listener that mounts them is M2. |
| `internal/client` | A `Service` implemented over HTTP. It is how every client reaches `cairn serve`. Built in M1: tested against an in-process `httptest` server (D19); M2 runs it against `cairn serve`. Its form is §5.4. |
| `internal/mcp` | MCP tools. Each tool calls `Service` through `internal/client`, which talks to `cairn serve`. It never touches the store. Built in M1. |
| `internal/cli` | Planned, M4. The CLI sub-commands for tasks, steps, events, learnings, directives, `recall`, `search` and `prime`, all with `--json`. Calls `internal/client`, never the store. |
| `internal/identity` | Project resolution from `CAIRN_PROJECT` and `.cairn.toml` (§2.1) is built. Agent and session resolution come later. |
| `internal/auth` | API keys and scopes (server mode). |
| `internal/ui` | The web pages, embedded in the binary. |
| `internal/conformance` | The conformance suite that checks any `Service`. |
| `cmd/cairn` | One binary with sub-commands. |

The `Service` interface is the contract. One test suite, the **conformance
suite**, runs against the local implementation, the HTTP client, and later
any other backend. A behaviour exists when the conformance suite says so.

### 5.2 Commands

| Command | Does |
|---|---|
| `cairn init` | Writes `.cairn.toml` in the current project, with `--project` and `--server`. Built in M1. It does not contact the server: the project is registered by its first call (CONTRACT §1.1). It is idempotent, and never changes a value the file has. |
| `cairn mcp` | The MCP stdio server. Requires `server` or `CAIRN_SERVER` and refuses to start without one (M1, D11). Built in M1: server only, through `internal/client`; it never opens a database and listens on nothing. |
| `cairn serve` | HTTP API and web UI. Flags for address, TLS and the database path. Planned, M2 (its handlers land in M1, D19); the web UI is M3. |
| `cairn hook <event>` | Output for client hooks. `session-start` is built in M1; `pre-compact` and `stop` are planned, M4. |
| `cairn status` | The project, the server in use and the active task, for a terminal. Built in M1. |
| `cairn export <task>` | A task as Markdown (`export task [id]`, built in M1). `export project` is in Scope of v1 and is not yet placed in a milestone. |
| `cairn key add/list/revoke` | API keys for server mode. Planned, M2. |
| `cairn enable --agent <name>` | Planned, M4. Installs a client's hooks (`session-start`, `pre-compact`, `stop`) and its registration. `claude-code` first; `opencode` and `pi` are written experimental. |
| `cairn backup <path>` | Planned, M2. A SQLite online backup of the database to a path. |
| `cairn restore <path>` | Planned, M2. Restores a backup, behind an interactive confirmation. |
| `cairn import bishop-memory` | One-time migration from a bishop-memory database (PLAN.md §5). Planned, M6. |
| `cairn doctor` | Checks the binary, the project file, the server and whether it answers as a Cairn server. Read-only. Built in M1; `--agent` is planned, M4. |

### 5.3 The server

Clients open no database. `cairn mcp` and the CLI send each call to
`cairn serve` over HTTP with the API key (D11). `cairn mcp` refuses to start
without a `server` (M1). The store stays inside `cairn serve`, and the web UI
is served by the same process. Until M2 the shipped binary has no network
listener, so M1's flow runs against an in-process server bound to loopback
(D19).

D2 designed a local backend, in which `cairn mcp` would open a SQLite file on
the machine. D11 cut it before it was built, so there is no choice of backend
and no switch in `.cairn.toml`.

A project's data does not move automatically. `cairn serve` can be pointed at
an existing SQLite file to make it the server's own database.

### 5.4 The HTTP wire form

`internal/api` serves the `Service` over HTTP, and `internal/client` is a
`Service` that calls it. Defined in M1 (2026-10-30). M2 serves this form on
`cairn serve`, and from then on it is public API. It is written here rather
than in CONTRACT.md because CONTRACT says what each call does for an MCP
client and the CLI. This section is how the adapter reaches the server.

- **Routes.** Every call is a `POST`. Agent calls go to `/v1/agent/<call>`,
  for the 15 calls of §4. Operator calls (CONTRACT §8) go to
  `/v1/operator/<call>`. The names are the snake_case tool names: `task_current`,
  `task_start` … `search`, and `learning_decide`, `directive_ratify`,
  `directive_retire`, `project_settings_set`. A path that is no call is `404`.
  A method other than `POST` is `405`, with `Allow: POST`.
- **Request.** A JSON object with the call's fields, as the request types in
  `internal/core` name them. `task_current` takes none: an empty body or `{}`.
  An unknown field, a value of the wrong type, a second JSON value, and a body
  over 1 MiB are refused: `400`, or `413` for the size. The server refuses
  unknown fields, so a misspelt field is not dropped.
- **Caller.** The headers `X-Cairn-Project`, `X-Cairn-Agent`,
  `X-Cairn-Session`, `X-Cairn-Client` and `X-Cairn-Role` carry the caller
  (CONTRACT §1.1). A call with no project is `invalid`. `X-Cairn-Role` is
  `operator` for an operator call and is an agent otherwise. M1's
  `api.UnauthenticatedHeaderCaller` trusts that header, with no key behind
  it. That is safe only while nothing listens. M2 replaces it with a resolver
  that takes the role from the API key's scope, so a client never sets its own
  role, and it does so before any listener exists (CONTRACT §1.1, M1 exception).
- **Success.** `200`, with the result as JSON, in the field names of the core
  types. Empty optional fields are left out (CONTRACT §1.3). A read of section
  `events` always carries `events`, as `[]` when the page is empty (CONTRACT §2).
- **Errors.** The status follows the kind, and the body is
  `{"error": {"kind": ..., "message": ..., "missing": [...]}}`, the same
  `core.Error` the call returned.

  | Kind | Status |
  |---|---|
  | `invalid` (including a malformed request, a bad path or a bad method) | 400, or 404 for a bad path, 405 for a bad method, 413 for a body over the cap |
  | `operator_only` | 403 |
  | `not_found` | 404 |
  | `refused` | 409 |
  | `internal` | 500 |

  `internal` is a wire kind, not a core kind. It is the answer to any error
  that is not a `core.Error`, and to a panic in a call, which the handler
  recovers. Its message is always `internal error`, so the caller never sees
  the cause. The cause goes to the failure hook that `api.Handler` is given
  (`FailureHook`, nil discards it), so the server can keep it for its own
  record. A `http.ErrAbortHandler` panic is not recovered.
- **The client.** It returns the `core.Error` the server named, with the same
  kind, message and missing list. Any other failure is a plain error that names
  the route and the status, and it is not a `core.Error`. The client ignores
  unknown response fields, so the server can add a field without a new prefix.
  Event data that is not JSON cannot be sent in a request body, so the client
  refuses that call before it is sent, with the message core gives for the
  same data in process (`core.CheckEventData`). The message is the same for the
  same data fault. With several faults the order differs: in process, a missing
  task id, an unknown kind or empty text is reported before the data; over the
  client, the data is checked first. A caller name that the headers cannot carry
  (a control character, or a space at either end) is refused by the client the
  same way, with core's message (`core.CheckCaller`), and is not sent.
  With no HTTP client given, `client.New` uses one with a 30-second timeout. A
  successful answer over 16 MiB is an error. The server does not cap its
  answers, so the cap is the client's guard.
- **Versions.** A breaking change takes a new prefix (`/v2`).

Tests: `internal/client` runs the whole conformance suite over an `httptest`
server on loopback that wraps the handlers over a real `core` (D19).
`internal/api` tests the statuses, the error body, malformed requests, the
headers and the events key.

## 6. Clients

### 6.1 Claude Code plugin

Lives in this repository, under `plugins/claude-code/`, published through a
marketplace file at the repository root.

```
plugins/claude-code/
  .claude-plugin/plugin.json   name "cairn"; userConfig: server (string),
                               api_key (string, sensitive)
  .mcp.json                    server "cairn": ${CLAUDE_PLUGIN_ROOT}/bin/cairn-launch mcp
  bin/cairn-launch             finds the cairn binary; if missing, exits
                               with the one-line install command
  hooks/hooks.json             SessionStart → cairn hook session-start
                               (built in M1; issue #16);
                               PreCompact and Stop → cairn hook pre-compact
                               and stop (planned, M4, D15)
  skills/cairn/SKILL.md        how to work with Cairn: the task loop below
  commands/init.md             /cairn:init → cairn init
  commands/status.md           /cairn:status
```

The plugin does not bundle the binary. A plugin's `bin/` is on the Bash
tool's PATH only, and shipping per-platform binaries in a plugin is not
documented, so the binary is installed once per machine and the launcher
finds it.

**The task loop the skill teaches:**

1. At session start, call `task_current`. If a task is active, continue it.
2. For new work, call `recall` with the goal, then `task_start` with a brief
   and plan.
3. For each step: `step_update` to start it, do the work, `step_update` to
   finish it with a summary. Log decisions and blockers with `event_log`.
4. When something is worth keeping, `learning_add`.
5. Close with `task_close`. If it is refused, supply what it lists.

A harness with its own doctrine (Bishop) replaces steps 2–5 with its own
rules, written in terms of the same tools.

### 6.2 Other clients

Plan v1 (decision D14) gives every client other than Claude Code the CLI over
the same HTTP API (`internal/cli`, planned for M4). Bash is enough for that, so
no per-harness plugin is needed in v1. The rows below are the per-client
registrations. They come through `cairn enable` in M4 (OpenCode and Pi
experimental, as PLAN.md §3 sets out). Per-harness plugins are Later.

| Client | Registration | Instructions | Notes |
|---|---|---|---|
| OpenCode | `opencode.json` → `"mcp": {"cairn": {"type": "local", "command": ["cairn", "mcp"]}}` | The skill text via `"instructions"` | Raise the default 5 s timeout. Tools arrive as `cairn_*`. |
| pi | `.pi/mcp.json` with `"exposure": "direct"` | `AGENTS.md` | The default `codemode` exposure hides the tools from the model. |
| Codex CLI, Gemini/Antigravity | `<client> mcp add cairn -- cairn mcp` | `AGENTS.md` | Not tested; listed for later. |
| Any MCP client | `cairn mcp` over stdio | The skill text | The contract is the tool list in §4. |

### 6.3 Bishop

Bishop becomes a crew pack: a Claude Code plugin in the bishop-harness
repository that depends on the `cairn` plugin. It ships the crew, the soul,
its skills and its lifecycle rules, written in terms of Cairn's tools, with
no state files of its own. That work belongs to the bishop-harness
repository. PLAN.md lists what Cairn must provide for it.

## 7. Review loop (optional, later)

Written fresh after the core is in use, guided by bishop-memory (PLAN.md, Later):

- **Classify**: a scheduled agent assigns a category and a confidence to each
  new finding.
- **Process**: a scheduled agent groups a category's findings, recommends
  accept, reject, supersede or defer for each, and drafts directive
  proposals.
- **Grade**: a scheduled agent grades each closed task A–F from its brief,
  steps, reviews and close record, with suggestions.
- **Review page**: the operator accepts or declines each recommendation and
  proposal in one place.

The runner is engine-neutral: any client that can run an agent with the
review-loop MCP profile can drive it. The agents never change a finding's
status; they only recommend.

## 8. Security

- Local mode (D2) was designed and cut by D11 before it was built. Its design
  listened on nothing and kept the database file under the user's control.
- `cairn serve` binds loopback by default and refuses to bind anything else
  without at least one API key. TLS by certificate files or behind a proxy.
- API keys are stored hashed. Three scopes, none built yet: `agent` (the MCP
  tools), `operator` (decisions and ratification), and `viewer` (read-only on
  every route; planned for M2 with a test that a viewer key gets 403 on every
  write). Plan v1 (decision D13) drops the `admin` scope from the design.
- An agent key cannot reach operator routes. The web UI asks for an operator
  key. A viewer key sees every page and gets 403 on every action (planned, M3).
- Request bodies are capped on every route.
