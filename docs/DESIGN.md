# Cairn Memory — Design

**Status as of 2026-10-29: phase 1 (contract) is done, and its exit check passed
with the operator's review of
[CONTRACT.md §9](CONTRACT.md#9-settled-here-for-review). Phase 2 (core and local
store) is done, and its exit check passed. Phase 3 (MCP adapter) is current. Live progress is on the
[milestones](https://github.com/djm56/cairn-memory/milestones) and the
[Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).**

The build order is in [PLAN.md](PLAN.md). The decisions in
PLAN.md §8 were adopted on 2026-10-27; this document marks each place that
rests on one as *(decision Dn)*.

## 1. What Cairn is

Cairn is a memory for coding agents. An agent records what it is working on,
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
   findings and grade finished tasks (PLAN.md phase 8).

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
- **Works alone, scales to a server.** A single user needs no daemon: the MCP
  adapter opens a local database *(decision D2)*. A team or several machines
  point at a `cairn serve` instance over HTTP. Both run the same code.
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

`.cairn.toml` is committed to the project, so everyone who works on it writes
under the same name. Its full contents:

```toml
project = "acme-api"          # required; lowercase letters, digits, hyphens
# server = "https://cairn.lan:8787"   # optional; absent means the local database
```

The API key never goes in this file. It comes from the plugin's secure option,
`CAIRN_API_KEY`, or `~/.config/cairn/client.env`.

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
outside transactions and writes inside `WithTx`.

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
  ratified_by, ratified_at, retired_at
```

Agents create learnings with status `proposed` only. Status changes and
directive ratification are operator routes, used from the web page or the
CLI, never MCP tools.

Global directives let one lesson apply to every project, which is how a
learning in one codebase reaches the next.

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

The step-review rule is deferred to phase 8 (#41).

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

| Tool | Purpose |
|---|---|
| `task_current` | The active task: brief, steps with display numbers, the last few events, and the directives that apply. The first call of a session. |
| `task_start` | Create a task with its brief (title, goal, context, constraints, criteria, key files) and, optionally, its plan. Returns the id. |
| `task_get` | One task in full, or one section of it (`brief`, `steps`, `events`, `close`). |
| `task_list` | Recent tasks in this project, filtered by status. |
| `task_update` | Status, priority, owner, next action, blockers; edit the brief before work starts. |
| `task_close` | Outcome, summary, criteria results, lessons, mistakes, deliverables. Refused with a list of what is missing when the close policy is not met. |
| `step_plan` | Replace the task's pending steps with a list. Steps already started are kept. A pending step that has an event or a review is not removed: the call is refused, and the agent skips that step first (CONTRACT §3). |
| `step_add` | Insert one step after a given step. |
| `step_update` | Start, finish, fail or skip a step; record an attempt and a summary. Also writes the matching event. |
| `event_log` | Append a journal event. |
| `event_list` | Journal for a task, newest first, paged. |
| `review_record` | A verdict on a step. Also writes a `review` event. |
| `learning_add` | Propose a finding, pattern or agent note. |
| `learning_list` | Learnings by status, kind, project or target. |
| `directive_list` | Directives that apply to this project, optionally to one agent role. |
| `recall` | Given a goal, return the directives, lessons, mistakes and similar past tasks that apply, across projects. |
| `search` | Full-text search across everything above. |
| `note_put` / `note_get` | Attach and read Markdown notes. |

Tools that do not exist on purpose: anything that changes a learning's
status, ratifies or retires a directive, deletes a task, or edits an event.

### 4.1 Session start

`cairn hook session-start` prints, for a SessionStart hook, a short block of
context: the active task (title, next action, current step), the count of
directives that apply, and one line telling the agent to call `task_current`.
When no task is active it says so in one line. It writes nothing.

## 5. Architecture

```
            ┌──────────── project ─────────────┐
            │ client (Claude Code, OpenCode…)  │
            │ cairn plugin: MCP entry, skill,  │
            │   session-start hook             │
            │ .cairn.toml                      │
            └───────────────┬──────────────────┘
                            │ MCP stdio
                    ┌───────▼────────┐
                    │   cairn mcp    │
                    └───┬────────┬───┘
         local backend  │        │  remote backend
          (no server)   │        │  (HTTP + API key)
                        │   ┌────▼─────────────┐
                        │   │   cairn serve    │
                        │   │ HTTP API, web UI │
                        │   └────┬─────────────┘
                    ┌───▼────────▼───┐
                    │  core service  │  validation, policy, identity
                    ├────────────────┤
                    │ store (SQLite) │
                    └────────────────┘
```

### 5.1 Packages

| Package | Job |
|---|---|
| `internal/core` | The `Service` interface and its implementation: every rule (close policy, status transitions, operator-only changes) lives here and nowhere else. |
| `internal/store` | SQLite access, migrations, FTS. Typed queries; no SQL in handlers. |
| `internal/api` | HTTP handlers. Thin: decode, call `core`, encode. |
| `internal/client` | A `Service` implemented over HTTP, for the remote backend. |
| `internal/mcp` | MCP tools. Each tool calls `Service`, so it works the same against the local or the remote backend. |
| `internal/identity` | Project, agent and session resolution (§2.1). |
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
| `cairn init` | Writes `.cairn.toml` in the current project and registers the project. |
| `cairn mcp` | The MCP stdio server. Local backend unless `server` or `CAIRN_SERVER` is set. |
| `cairn serve` | HTTP API and web UI. Flags for address, TLS and the database path. |
| `cairn hook <event>` | Output for client hooks (`session-start`). |
| `cairn status` | The active task and the backend in use, for a terminal. |
| `cairn export <task>` | A task as Markdown. |
| `cairn key add/list/revoke` | API keys for server mode. |
| `cairn import bishop-memory` | One-time migration from a bishop-memory database (PLAN.md §5). |
| `cairn doctor` | Checks the binary, the config, the database or server, and the project file. |

### 5.3 Local and remote backends

- **Local:** `cairn mcp` opens `~/.local/share/cairn/cairn.db` (or
  `CAIRN_DB`) in WAL mode with a busy timeout. Several MCP processes on one
  machine share it safely. Task-id allocation is one transaction. No daemon
  is needed. `cairn serve` on the same database shows the web UI.
- **Remote:** `cairn mcp` sends each call to `cairn serve` with the API key.
  Used for a home server, a team, or several machines.

Switching is one line in `.cairn.toml` or one environment variable. A
project's data does not move automatically; `cairn serve` can be pointed at
the local database file to make it the server's.

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

Written fresh after the core is in use, guided by bishop-memory (PLAN.md phase
8):

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

- Local mode listens on nothing. The database file is the user's.
- `cairn serve` binds loopback by default and refuses to bind anything else
  without at least one API key. TLS by certificate files or behind a proxy.
- API keys are stored hashed. Scopes: `agent` (the MCP tools), `operator`
  (decisions and ratification), `admin` (keys and projects).
- An agent key cannot reach operator routes. The web UI asks for an operator
  key.
- Request bodies are capped on every route.
