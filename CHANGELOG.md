# Changelog

All notable changes to Cairn are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Each pull request
adds its entry under Unreleased, unless it carries the `skip-changelog` label
([CONTRIBUTING.md](CONTRIBUTING.md#keeping-docs-current)).

## [Unreleased]

### Added

- Phase 0 skeleton: the Go module, the `cairn` command with `version` and
  `help`, the Makefile, CI on pull requests, and the contributing rules
  ([PR #1](https://github.com/djm56/cairn-memory/pull/1)).
- Phase 1 contract: the `Service` interface and the request and result types
  in `internal/core`, the call-level contract in `docs/CONTRACT.md`, and a
  conformance suite that fails against a stub
  ([PR #2](https://github.com/djm56/cairn-memory/pull/2)).
- GitHub project management: labels, the roadmap, the
  bootstrap script, issue and pull request templates, the docs check, the
  wiki and its publish workflow, and branch protection on `main`
  ([PR #3](https://github.com/djm56/cairn-memory/pull/3)).
- Phase 2 store: the schema v1 of DESIGN §3 as numbered, embedded migrations,
  each applied in its own transaction and recorded in `schema_migrations`; WAL
  mode, a busy timeout and foreign keys on every connection; the `search_index`
  FTS5 table with its triggers; and `store.Open` on `modernc.org/sqlite` v1.61.0.
  The module moves to Go 1.26, which that driver's dependency graph requires.
  ([PR #45](https://github.com/djm56/cairn-memory/pull/45)).
- Phase 2 store queries: typed functions over every table of DESIGN §3, a
  `WithTx` write-transaction helper, a task id allocator, scoped search, and
  migration 0002 with the secondary indexes and the `tasks.project` guard
  (issue #8, [PR #47](https://github.com/djm56/cairn-memory/pull/47)).
  Several processes opening one new database at once no longer fail with `SQLITE_BUSY`.
- Phase 2 identity: `internal/identity` resolves the project from
  `CAIRN_PROJECT`, then the nearest `.cairn.toml` walking up from
  `CLAUDE_PROJECT_DIR` or the working directory, and refuses a call with no
  project. An invalid name is refused, not skipped. The file is read with
  `github.com/BurntSushi/toml` v1.6.0 ([PR #49](https://github.com/djm56/cairn-memory/pull/49)).
- Phase 2 core, issue #9: `core.Local`, the local `Service`, implements
  the task and step calls of CONTRACT.md §2 and §3 over the store. Each write
  runs in one transaction, and the one-active rule, the brief freeze, the
  status moves, the step positions and the journal rows of `step_update` and a
  blocked or abandoned task follow the contract. The conformance suite runs
  against it: 14 cases pass, and the 21 cases for events, learnings, search,
  operator calls and the close policy are skipped with their part named.
  `conformance.RunWith` is the skip list
  ([PR #51](https://github.com/djm56/cairn-memory/pull/51)).
- Phase 2 core, issue #9: `core.Local` implements the journal
  (`event_log`, `event_list`, `review_record`), learnings (`learning_add`,
  `learning_list`), `directive_list` and the directives of `task_current`,
  notes (`note_put`, `note_get`), and `search` and `recall` over every kind of
  row. The conformance suite has 22 cases passing and 13 deferred (operator
  calls, `task_close` and the close policy). `RunWith` fails a
  deferred case that passes, so a stale entry is caught
  ([PR #53](https://github.com/djm56/cairn-memory/pull/53)).
- Phase 2 core, issue #9: `task_update`
  never moves a task into `open`; `step_plan` over a pending step with an event or
  a review is refused, and the way out is to skip the step; `search` does not hit
  retired directives. Recall keeps short upper-case and digit words of a goal
  (`JWT`, `CSS`, `v2`), and `StepHasRecords` is removed. The conformance suite has
  38 cases: 24 pass and 14 are deferred
  ([PR #54](https://github.com/djm56/cairn-memory/pull/54)).
- Phase 2 core, issue #9: `core.StepRef` fields are
  pointers, so `{}` is `invalid` in every call that takes a step ref, and
  `{"number": 0}` is valid only as `step_add`'s `after`. `IDRef` and `NumberRef`
  build refs. Recall's stop list drops the short function words that a goal in
  capitals passed as names. Review fixes follow: the §1.5 diagram,
  the CONTRACT header counts (25 of 39 cases pass), the retired-directive and
  review-branch conformance coverage, and DESIGN §4's `step_plan` row
  ([PR #55](https://github.com/djm56/cairn-memory/pull/55)).
- Phase 2 core: the close of a task and the operator's decisions. `core.Local`
  now implements `task_close` with the project's close policy, and the four
  operator calls: learning decisions, directive ratification and retirement, and
  project settings. A refused close changes nothing and lists what is missing.
  An agent that calls an operator call gets `operator_only`. All 39 conformance
  cases pass, so issue #9 is complete once its pull request chain lands on
  `main` ([PR #56](https://github.com/djm56/cairn-memory/pull/56)).
- M1 HTTP wire form (issues #14, #17): `internal/api` serves `core.Service` as thin handlers, one POST per call, with each error answered at a status for its kind and a body of kind, message and missing list. `internal/client` is a `Service` over that form. The whole conformance suite runs through the client over an in-process loopback server, and all 80 cases pass. The form is in DESIGN §5.4. The shipped binary has no listener (D19).
- M1 MCP tools (issue #14): `internal/mcp` registers the fifteen tools of DESIGN §4 (the thirteen tools, plus `recall` and `search`) over a `core.Service`, through the MCP protocol of `github.com/modelcontextprotocol/go-sdk` v1.8.0. A call's result is compact JSON; a failure is a tool error whose text is the HTTP error body, with its kind, message and missing list. The step tools name the step's assignee `assignee`, since `agent` is the caller there (CONTRACT §3).
- M1 `cairn mcp` (issue #15): the MCP server over stdio, through `internal/client` to a server. It is the only client path (D11), and it never opens a database. It needs `server` in `.cairn.toml` or `CAIRN_SERVER`, and refuses to start without one (issue #66). It listens on nothing (D19).
- Server resolution (issue #66): `internal/identity` gives the server URL from `CAIRN_SERVER`, else from the `server` key of the nearest `.cairn.toml`.

- M1 CLI (issues #16 and #69): `cairn init --project <name> --server <url>` writes `.cairn.toml` in the project directory (`CLAUDE_PROJECT_DIR` when Claude Code sets it, else the working directory), writes the file whole or not at all where the filesystem has hard links (on one without them it writes the file directly, which a crash can leave partly written), is idempotent, and never overwrites a value it finds. It creates `.cairn.toml` with mode 0644 before the umask, which gives 0600 under umask 077. It does not contact the server, and it refuses a server URL with a user name or password, since the file is committed. `cairn init --help` prints to stdout. `cairn status` shows the project, the server and the active task. `cairn doctor` checks the binary, the project file, the server and whether it answers as a Cairn server, and changes nothing. `cairn export task [id]` prints a task as Markdown, the active task by default. `cairn hook session-start` prints the active task for a session start, and exits 0 so a session always starts. Every command except `init` reads through the server; none opens a database or listens. `cairn export project`, `doctor --agent`, and the `pre-compact` and `stop` hooks are M4 or later.
- M1 exit check (issue #13): an MCP stdio test drives a task through `cairn mcp`, `internal/client`, `internal/api` and `core` against an in-process server, closes it, and `cairn export task` prints it. The stdio flow test (issue #18) drives the whole flow through `cairn mcp` and asserts the result of each call.

- M2 Server, built; its live run (issue #19) passed on 2026-11-02. `cairn serve` runs the M1 handlers over HTTP, on loopback by default, with TLS by files or behind a proxy, body caps and `/healthz`. API keys have `agent`, `operator` and `viewer` scopes, made, listed and revoked with `cairn key`. `cairn backup` and `cairn restore` copy and replace the database, and the directive cap and `review_by` are in. The setup is in [docs/DEPLOY.md](docs/DEPLOY.md), and the operator's live run is in [docs/M2-LIVE-RUN.md](docs/M2-LIVE-RUN.md). The entries below record each step.
- M2 step 3, API keys (issue #110, [PR #122](https://github.com/djm56/cairn-memory/pull/122)): migration 0004 gives `api_keys` one scope per key and `revoked_at`; `internal/auth` makes and hashes keys and resolves a bearer key to its scope; `api.KeyCaller` takes the role from the key, never from `X-Cairn-Role`. A missing, malformed, unknown or revoked key is `401 unauthenticated`, and a viewer key is refused on every write with `403 read_only`. Each route is marked read or write.
- M2 step 5, `cairn serve` (issues #110 and #112, [PR #123](https://github.com/djm56/cairn-memory/pull/123)): the server's listener. `--db` names the SQLite file and is required; `--addr` defaults to `127.0.0.1:8788`. `--tls-cert` and `--tls-key` go together, and `--behind-proxy` accepts a proxy that terminates TLS. Any address other than loopback needs TLS or that flag, and at least one active key. A loopback server with no key starts, and every call is refused until a key exists. `GET /healthz` needs no key. The failure hook logs the cause of each internal failure, and never the header, the body or a key. A viewer's read of a project that does not exist is `not_found` and writes no row (CONTRACT §9 item 44). An operator route refuses an agent key before it reads the body, with `403 operator_only`. A 401 names the Bearer scheme. Migration 0004 is not yet on `main`. It drops and recreates the empty v1 `api_keys` table, and refuses to run if that table holds any row, so no key is dropped unseen.
- M2 step 7, `cairn key add`, `list` and `revoke` ([PR #124](https://github.com/djm56/cairn-memory/pull/124)): API keys made in the server's database. `add` prints the key once, and `list` never shows a hash or a key. The database is created 0600. The client sends `Authorization: Bearer <key>` on every call, from `CAIRN_API_KEY` or from `~/.config/cairn/client.env`, which is refused unless only its owner can read it. `cairn mcp` refuses to start with no key, and `cairn doctor` reports where the key was found. The default port is now 8788, since 8787 is memoryd's. Also: `cairn serve` refuses `:memory:` and `file:` names, warns when `--behind-proxy` serves without TLS, and stops the drain on a second Ctrl-C.
- M2 step 9, exit tests and review fixes ([PR #125](https://github.com/djm56/cairn-memory/pull/125)): the M2 exit checks run against a real `cairn serve` listener. The conformance suite passes over HTTP, an agent key is refused on every operator route, a viewer key is refused on every write route (#73), and the M1 flow runs with `server` set. The client never follows a redirect, so a key is not sent to a second address. The reach guard keeps the subcommand names to their dispatch table. `key revoke` checks its name, and `key add` revokes a key it cannot show and names the database it creates. A `client.env` may begin each line with `export`, and must be a regular file; a symlink is judged by its target. `cairn doctor` warns when a key would go over `http` to a host that is not loopback.
- M2 step 11, directive cap and ranking (D16, issues #76 and #77, [PR #127](https://github.com/djm56/cairn-memory/pull/127)): `task_current`, `directive_list` and `recall` return at most 12 directives, ranked agent, project, then global, and newest first. A directive past its `review_by` is expired: it is left out and counted in `expired`, and the cap's excess is counted in `omitted`. `DirectiveRatify` takes an optional `review_by` day (YYYY-MM-DD, UTC, after today), 90 days from ratification by default. Migration 0005 adds `review_by` and backfills active directives. The subcommand guard covers the dispatch names in `main.go`; the exit loops send forged role headers; `key add` revokes a key when stdout is a closed pipe; the redirect tests prove the target is reached.
- M2 step 13, `cairn backup` and `cairn restore` (issue #74, [PR #128](https://github.com/djm56/cairn-memory/pull/128)): `cairn backup --db <file> <out>` writes a consistent copy of a running server's database to a new file at mode 0600, and refuses an existing file. `cairn restore --db <file> <backup>` checks the backup first, refuses while the database is in use, and requires the database path to be typed at a terminal, with no flag to skip it. It folds the database's `-wal` into it, then moves the database to `<db>.pre-restore-<UTC time>`, with any `-shm` beside it, and never deletes anything; a file that cannot be read as a database is moved as it is. A restore that stops after a move names the pre-restore path and the steps that put the database back (README and DESIGN §5.2.1, "If a restore fails"). `cairn serve` and `cairn key add` refuse to create a database over an interrupted restore. The directive reads list only the caller's own role's agent-scoped directives, so another role's cannot fill the cap of 12; `directive_list` with no agent still lists every role. `cairn status` and the session-start hook show the `expired` and `omitted` counts when they are not zero, and say that agent-scoped directives are not counted, since the CLI names no agent role. The `.cairn.toml` form of the M1 flow test runs again. Also: a directive is due at 00:00 UTC of its `review_by` day.
  **Upgrade note.** Migration 0005 gives every active directive a `review_by` 90 days after the upgrade runs, whatever its `ratified_at`, so the upgrade expires no directive. To extend one, retire it and ratify it again with the `review_by` you want (CONTRACT §8). Migration 0005 was revised before its first release. Main carries only migrations 0001 to 0003 before this release, so no released build applied the earlier form. The earlier form backfilled `ratified_at` plus 90 days ([PR #127](https://github.com/djm56/cairn-memory/pull/127)), and this release backfills from the upgrade, as above.
- DEPLOY.md: an Apache reverse-proxy site for setup (b), with the certificate from `certbot --apache`. ([PR #131](https://github.com/djm56/cairn-memory/pull/131))
- Docs: M1 is recorded as passed, M3 (Web UI + dashboard) is current, and #72 is placed in M4 (D22), `cairn export project` in M4 Hooks + CLI.

- M3 design, docs only: the web UI, its sessions, CSRF and headers (DESIGN §5.5),
  four new operator and viewer reads and the `DirectiveReaffirm` call (CONTRACT
  §8, §8.1, §9 items 50 to 53), and decisions D23 to D26 (PLAN §8, §10)
  ([PR #150](https://github.com/djm56/cairn-memory/pull/150)).
- M3 web UI sign-in and sessions (D23): `cairn serve` serves `/ui/` beside the API. An operator or viewer key signs in with a password field, and an agent key is refused. The session is a random token in an HttpOnly, SameSite=Strict cookie, named `__Secure-cairn_ui` over TLS or behind a proxy. Its server side keeps only the hash, and it ends at 12 hours or after 2 hours idle, or at the next request once its key is revoked. Forms are checked for session, origin, role, body and CSRF, in that order. Every answer under `/ui/` carries a content security policy, `nosniff`, `same-origin` referrer policy and `no-store`. Migration 0006 adds `ui_sessions` and `directive_reaffirmations`. The dashboard is a placeholder until M3's later steps.

- M3 reads for the web UI (CONTRACT §8.1, §9 items 50 and 51): `project_list`,
  `task_browse`, `directive_browse` and `dashboard` in core, and under
  `/v1/operator/` on the wire, for an operator or a viewer key. An agent key is
  refused with `operator_only`. None creates a project row. `learning_list` with
  `all_projects` needs no project for an operator or a viewer. Migration 0007 adds
  the indexes these reads use. The client implements the four reads.
- M3 web UI read pages (DESIGN §5.5): the dashboard, the tasks list and a task's
  record, learnings for one project and across projects, the directives, the project
  list and each project's settings. The pages read through core in process, for an
  operator and a viewer alike. The action forms are listed below. A project page checks its project first and answers 404 for a name with
  no row, and creates none. Stored text is escaped, and multi-line text keeps its line
  breaks. Every page has the header, with the signed-in key, its role, a link to each
  page, and sign-out.
- M3 web UI actions (DESIGN §5.5): an operator can decide a proposed learning, ratify a
  directive (from an accepted learning, by a link on it, or from scratch), retire a
  directive with a confirmation, reaffirm its review day, and save a project's display
  name, parallel-task setting and close policy. A viewer sees no action form and gets
  `403` `read_only` on each action and on the ratify form. Every action form carries a
  CSRF field. A ratify into a project, and a settings save, name a project that must
  already exist, so a misspelt name creates no project.
- M3 `DirectiveReaffirm` (CONTRACT §8, §9 item 52, D26): an operator sets the `review_by`
  of an active directive in place. The code, rule and ratification stay, and one history
  row is written in the same transaction. A retired directive is refused, and an expired
  one is reaffirmed. On the wire it is `/v1/operator/directive_reaffirm`.
- M3 exit check (PLAN M3): a lead sees a proposed finding, accepts it, ratifies a
  directive from it, and the next session's `task_current` returns that directive. A
  viewer key sees every page and is refused every action. The check runs over a real
  listener, store and core.
- Docs: M3's exit check passed and was recorded on 2026-11-03, and M4 Hooks + CLI is current.
- Docs: `docs/M3-UI-CHECK.md`, an optional hand-test guide for the web UI through a real
  proxy: the upgrade and its rollback, the headers and cookie, and the exit-check flow.
  Linked from the README's Web UI section and from DEPLOY.md.
- Docs: DEPLOY.md's same-host setup (b) now adds `--behind-proxy` through a unit override, since the shipped unit does not pass it and the web UI refuses every form without it.
- Docs, M3 follow-ups (#138, #168): DEPLOY.md says that the `listening` line's `active_keys` is the count at start time, and that log times are UTC. Setup (b) offers `docs/deploy/cairn-behind-proxy.conf`, a systemd drop-in with `--behind-proxy`, as an alternative to `systemctl edit`. The shipped unit is unchanged.
- Docs, M3 follow-ups: CONTRACT §8 scopes "does not create a project" to the two operator calls, and §1.1 says an operator-role read still creates the row. The web UI's pre-check only chooses the response form (DESIGN §5.5).
- M3 follow-ups (#115): `make scan-invisible` lists each tracked file that holds a bidirectional
  override or isolate, a zero-width character, a word joiner or invisible operator, or a byte-order
  mark, and exits non-zero when any is found. CI runs it on every pull request.
- M3 follow-ups (CI): CI cross-builds the module for Windows and macOS on every pull request, so a
  platform break is caught. The cross-build does not make Windows a supported target.
- Docs, M3 follow-ups (#134): CONTRACT §2 says that a `done` close is allowed while steps are still `pending`, and that only an `active` step blocks it. A `done` close over pending steps keeps those steps `pending`, and `cairn export task` prints them with their status.
- Docs, M3 follow-ups (#147): DEPLOY.md restarts the unit with `systemctl restart` when the proxy override is added to a unit that is already running, and makes a replacement key before revoking the old one, since a server that listens beyond loopback refuses to start with no active key. M2-LIVE-RUN.md step 6.1 says the same.
- Docs, M3 follow-ups (#148): DEPLOY.md gives the `systemd-analyze security` result as a description of the score, not as verbatim output.
- Docs, M3 follow-ups (#165): the review labels in code comments and in the public docs are replaced by the rule they stood for, and the comments that were awkward or unwrapped are reworded. Two test-case name strings keep their review labels by design, since renaming them changes code: internal/core/errortext_test.go:117 and internal/store/queries_test.go:988. No code token changed. DESIGN.md §5.2.1 drops a review label, and DEPLOY.md says what a backup swap can leave behind, including an empty `.cairn-backup-*` directory from a refused backup.
- Docs, M3 follow-ups: CONTRIBUTING.md lists `scan-invisible` and the Windows and macOS cross-builds among what CI runs. CLAUDE.md no longer names a local checkout path. wiki/Decisions.md records D27 to D29 as Bishop defaults, pending operator approval.
- M4 CLI (#87, partial): `cairn task current|get|list`, each with `--json` (one line, the object the MCP tool returns). The text rules and error text move to `internal/cli`. More verbs to follow.
- M4 CLI, task reads in full (#87): `cairn task get` prints every section it read, with the key files, deliverables and directive text that the first form left out, and `task current` prints the criteria, the steps, the recent events and each directive's code, scope, title and rule. The text form is a summary and `--json` is the whole object. `--json` writes DEL, the C1 controls and the bidi marks as `\uXXXX` escapes, and the output still decodes to the same value. `cairn task get <id> --step <n>` filters the events to one step. After `--`, every argument is positional. `--limit 0` and an empty `--status` are usage errors, and `--status` says to leave it out to list all. The write verbs take the calling agent from `--agent`, or from the `CAIRN_AGENT` variable when the flag is left out; `task current` reads the same name as the caller's role.
- M4 CLI, task start (#87): `cairn task start --title t --goal g [--context c] [--constraints k] [--priority p] [--queue] [--json-input] --agent a [--json]` creates a task with its brief and plan through `task_start`. Its criteria, key files and steps come from standard input as one JSON object with `--json-input`, and a text flag set to `-` reads standard input. Its agent is `--agent`, or `CAIRN_AGENT`.
- M4 CLI, step add and update (#87): `cairn step add <task-id> --title t [--after n] [--assignee a]` inserts a step through `step_add`, with `--after 0` for the first place, and `cairn step update <task-id> <number> --action start|finish|fail|skip [--summary s] [--assignee a]` changes one through `step_update`. A step is named by its number, as the steps list shows it. Both take `--agent` or `CAIRN_AGENT`.
- M4 CLI, event log (#87): `cairn event log <task-id> --kind k --text t [--step n] [--data d] --agent a` writes a journal entry through `event_log`. `--data` is a JSON object, such as a review's verdict, critical and warnings, and `-` reads it or the text from standard input. The server refuses the kinds it writes itself, `started`, `finished` and `close`.
- M4 CLI, task close (#87): `cairn task close <id> --outcome done|failed --summary s [--no-lessons-reason r] [--json-input] --agent a` closes a task through `task_close`. Its criteria results, lessons, mistakes and deliverables come from standard input with `--json-input`, where a list left out is absent and `[]` says there are none, as the close policy reads them. A refused close lists what is missing and changes nothing.
- M4 CLI, step plan (#87): `cairn step plan <task-id> --json-input --agent a` replaces the task's pending steps through `step_plan`, with the list of `{"title", "agent"}` on standard input. `--json-input` is required and `steps` must be present, so `[]` removes the pending steps only when asked for. A step an event refers to is not removed, and the refusal names it.
- M4 CLI, task update (#87): `cairn task update <id>` changes the fields given and no others, through `task_update`: `--status` (active, blocked or abandoned), `--reason`, `--priority`, `--owner`, `--next-action`, `--blockers`, and the brief fields `--title`, `--goal`, `--context` and `--constraints`, which the server refuses once a step has started. Given blank, `--owner`, `--next-action`, `--blockers`, `--context` and `--constraints` clear their fields. The lists, `criteria` and `key_files`, replace the whole list and come from `--json-input`.
- M4 CLI, usage checks (#87): `event log --data` must be one JSON object, and a value that is not is a usage error that names `--data`. An agent name the wire form cannot carry (a control character, or a space at either end) is refused before the call, and the message names `--agent` or `CAIRN_AGENT`. Every write verb checks its agent and its number ranges before it reads standard input, so a usage error leaves piped text unread. `--json` escapes every Unicode format character, not only the bidi controls.
- M4 CLI, steps by id (#87): `step update <task-id> --step-id n` and `event log --step-id n` name a step by its id, as CONTRACT §1.4 does, in place of its number or of `--step`. Giving both forms is a usage error.
- M4 CLI, agent-scoped directives (#87): `cairn status` and `cairn hook session-start` count the directives of the role named by `CAIRN_AGENT`, when it is set to a valid name. With a name the wire form refuses, they say that `CAIRN_AGENT` is set but refused; with it unset or blank, they say that no agent role is set.
- M4 CLI, learning (#87): `cairn learning add --kind finding|pattern|agent-note --title t --body b [--task-id id] [--target t] [--rationale r] [--example e] --agent a` proposes a learning through `learning_add`, stored as proposed, and `cairn learning list [--status s]... [--kind k]... [--target t] [--limit n] [--cursor c]` reads the project's learnings through `learning_list`, newest first. The text fields of add take `-` to read standard input. Both take `--json`. The list leaves out `all_projects` on purpose: the CLI lists one project at a time. It is not a role gate, since an agent with a project can read every project's learnings through the server.
- M4 CLI, directive list (#87): `cairn directive list [--agent role]` lists the directives that apply, ranked and capped at 12 as `directive_list` gives them, with the expired and omitted counts. `--agent` narrows the agent-scoped directives to one role. It is not read from `CAIRN_AGENT`, so the listing of every role stays available. Takes `--json`.
- M4 CLI, recall (#87): `cairn recall --goal text [--agent role] [--limit n]` answers what applies to a goal through `recall`: the directives of the project and the role, then the lessons, mistakes and tasks that match, from every project. `--agent` names the caller's role, as the MCP argument does, and `CAIRN_AGENT` names it when the flag is left out. `--goal -` reads the goal from standard input. A name the wire form refuses, from `--agent` or `CAIRN_AGENT`, is a usage error (exit 2) here, while `status` and the session-start hook only note a refused `CAIRN_AGENT`. Takes `--json`.
- M4 CLI, search (#87): `cairn search --query text [--kind k]... [--all-projects] [--limit n] [--cursor c]` finds rows by plain words through `search`, with the hit's kind, id, project, title and snippet. `--kind` takes task, event, learning, directive, note, lesson or mistake, repeated, and an unknown one is a usage error before the call. `--query -` reads the text from standard input. In a snippet, the words in square brackets are the matched words. Takes `--json`.
- M4 CLI, prime (#87): `cairn prime [--agent role]` is the session start for an agent with only a shell and no hook. It writes the block the session-start hook writes, closed by a pointer to the brief, then the active task as `cairn task current` writes it, without the directive counts the block gives. When directives are left out, the block names `cairn directive list`. `--agent` names the caller's role, with `CAIRN_AGENT` as the fallback. Takes `--json`, which writes the project, the role and the `task current` result.
- M4 hook, pre-compact (#83): `cairn hook pre-compact` prints the session-start block: the active task, its next action and current step, and the directive count. It makes one read, exits 0 on every fault and says so on stderr, as `session-start` does.
- M4 hook, stop (#84): `cairn hook stop` prints one JSON object on one line, `{"systemMessage": ...}`, when a step of the active task is active with no summary, and nothing otherwise. The message names the step; when several steps are in that state, it gives the count and names the first. A step that starts again after a failure keeps its failed summary, so it is not named. It makes one read, exits 0 on every fault and says so on stderr, as the other hooks do.

### Changed

- Behaviour change, M4 CLI (#87): `cairn learning list --kind` refuses a kind that is not finding, pattern or agent-note as a usage error, exit 2, before any call, where the server used to answer. `cairn directive list` prints `no directives` for an empty list, where it printed `no directives apply`, so a script that matches the old words must change. `cairn task list` and `cairn learning list` print `no tasks` and `no learnings` when they hold nothing, where they printed nothing.
- Behaviour change, M3 (issue #163): `directive_retire` refuses a code of the wrong form (not `D-` and four or more digits) as `invalid`, as `directive_reaffirm` already did. Before, a malformed code was `not_found`. A well-formed code with no row is still `not_found`.
- Behaviour change, M3 (issue #164): an operator call that names a project with no row, a `directive_ratify` of project scope or a `project_settings_set`, is refused as `not_found` in core, for every caller (CONTRACT §8). Before, only the web UI refused it, and core created the row. Agent calls still create a project's row on its first call.
- Behaviour change, M3 follow-ups (#121): a 404 whose body is not a cairn answer, such as a reverse proxy's page, names the base URL the client was given and says the body was not a cairn answer, as the `no_route` message does. Other statuses are unchanged.
- Behaviour change, M3 follow-ups (#136): `cairn key add`, `list` and `revoke` with no `--db` name the systemd unit's database, `/var/lib/cairn/cairn.db`, in the message and in `--help`.
- Behaviour change, M3 follow-ups (#137): the server's log lines carry UTC times, as `cairn export` and `cairn key list` do. Before, they carried the host's offset.
- Behaviour change, M3 follow-ups (#160): the server's internal-failure log line quotes the route path and cuts it to 200 bytes, as the web UI's log lines do. A path with a line break can no longer start a second log line.
- Behaviour change, M3 follow-ups (#129): `cairn backup` makes its copy in a directory it creates beside the output, and gives the copy its name by a hard link, which refuses any existing name, a symbolic link included. A link placed at the output name while the copy is made is refused, and nothing is written through it. Before, the copy was written by name after an empty file was made, so a link put in that gap could redirect it. The output is still owner-only and is never overwritten.
- Behaviour change, M3 follow-ups (#129): `cairn backup` removes its staging directory through a handle bound when the directory is made, and refuses the backup when the staging name has been swapped during the copy, the link or the cleanup. Before, a rename of that directory could make the cleanup delete files in another directory. The copy is made under umask 077, so it is owner-only wherever it is written. The output directory must still not be writable by other users (docs/DEPLOY.md).
- Behaviour change, M3 follow-ups (PR #172): `cairn backup` needs a filesystem that supports hard links. When the link fails because the filesystem does not support one (EPERM, ENOTSUP, EOPNOTSUPP, EXDEV or ENOSYS), it fails with a message that says so. Any other link failure is reported as the link error, without that clause. Before, every link failure carried the clause.
- Behaviour change, M3 follow-ups (PR #173): `cairn backup` checks its staging directory after the copy, as well as before the copy and before the link. A swap of the staging name during the copy is refused at that check, and the copy is removed through the directory the backup made (docs/DESIGN.md §5.2.1). docs/DEPLOY.md says what a swap can leave behind.
- Behaviour change, M3 follow-ups (PR #172): the `mv` command in the stray-file refusal and in the start-fresh text reads `/path/to/folder`, not `<folder>`, so a pasted command does not act as a shell redirect. When a backup's staging directory cannot be removed after the link, its error names that directory, which may hold a partial copy.
- Behaviour change, M3 follow-ups (#140): `cairn restore` into a path with no database refuses when a `-wal` or `-shm` is beside that path, and names the files. It no longer moves them aside. Nothing is moved or deleted; the refusal says to check the files and move them out of the folder by hand, then run the restore again.
- Behaviour change, M3 follow-ups (#140): the refusal to create a database over the files of an earlier restore now says how to start fresh: check the files, move them out of the folder yourself, then start `cairn serve` again. Nothing is deleted. The refusal is otherwise unchanged.
- Behaviour change, M3 follow-ups (#141): `cairn key add` into a folder that does not exist gets the plain missing-folder error. Before, it got the message about an interrupted restore.
- Behaviour change, M3 follow-ups (#156): the header's project switcher lists the first ten projects, in name order, from one bounded read. When more exist, it links to the projects page with "more…". Before, every page read every project and listed them all.
- Behaviour change, M3 follow-ups (#161): a ratify whose `from_learning` names no learning re-renders the form at `400`, with the typed values and the message "from_learning names no learning". Before, it answered `404` and the typed values were lost.
- Behaviour change, M3 follow-ups (#162): an `operator_only` error from core reaches the UI's `internal` answer (`500`), as an unknown kind does. Before, a page showed "This page is for an operator or a viewer key." with `403`. No page or action can reach it, since the scope is checked before core is called.
- M3 web UI: `/ui/tasks` filters on every repeated `status` in its query, as DESIGN §5.5
  gives it. Before, it read the first one only.
- Behaviour change, M3: a project whose stored settings are `'{}'`, or leave a close-policy switch out, now has the missing switches on in `task_close`, as `project_list` already showed them. Before, the close rules read a missing switch as off, so a close with no summary could pass on such a project. Every switch the stored object does set keeps its value. The one decoder is shared by the reads and the close rules (DESIGN §3.6, CONTRACT §8.1). New rows are stored with the full object, so only a row written by hand or a test seed is affected.
- M3 fixes for the web UI sign-in: a path the mux cleans first,
  such as `//ui/x`, now carries the UI's headers on the mux's own redirect. The UI's
  log lines quote the path, and the redacted cause of a render failure is logged
  without any key. The role check of a form fails closed. The departures of DESIGN §5.5 are written down, and migration 0007 indexes
  `ui_sessions.last_seen_at` and `directive_reaffirmations.directive_id`.

- M1 review fixes and the stdio flow test (issues #14, #15, #16, #18, #66, #69, #70). The source guard checks the non-test code of every package in the module except `internal/store`, `internal/core` and `internal/api`, which are exempt by name with their reasons. It refuses, in those packages, an import of the store or of the test server, a dot import, a listener function or method of `net`, `net/http` and `crypto/tls`, and a reference to an HTTP server. The error-text audit reads `core.Invalid` and its siblings outside core, and the messages `cairn doctor` prints. Server text that the hook, `status`, `export` and `doctor` print, error messages included, is one printable line: line breaks become spaces, control characters (Unicode category Cc) and format characters (Cf, such as bidi overrides, isolates and zero-width characters) are removed, and escape sequences are removed whole. The hook, `status` and `doctor` cap that line at 200 runes, as does an error message of `export`; the single-line fields of an export are not capped. `export` keeps the line breaks of a section and removes its control and format characters. The hook exits 0 after a panic, and its bound is tested against a server that never answers. `cairn init` writes atomically where the filesystem has hard links, falls back to a direct write where it has none, refuses credentials in the server URL, writes where identity reads, and refuses to run from a working directory outside `CLAUDE_PROJECT_DIR`; below a file with the same values, it answers that nothing changed. `cairn status`, `cairn doctor` and the client print a server URL without its user name and password. A session name is checked like the agent and client names. A panic in an MCP tool writes its stack to stderr, and the model gets only the cause. The conformance suite has 87 cases.
- M1 review fixes (PR #104): an agent or client name with a control character, or a space at either end, is `invalid` on every call, and the client refuses it before it sends (CONTRACT §1.1). A source guard checks the non-test code of `cmd/cairn`, `internal/mcp`, `internal/client` and the other packages it does not exempt by name, and fails on the store and listener forms it names; it also fails on any non-test use of the unauthenticated caller. Results keep `&&`, `->` and `<` unescaped. A panic in an MCP tool is an internal tool error. A refused write with no agent names the `agent` argument. An unresolved project says to restart the session after the fix. The error-text audit reads `internal/identity`. The conformance suite has 86 cases.
- M1 wire form, the review warnings of PR #103: event data that is not JSON is refused with core's own message, on the client as in process, and is not sent; `core.CheckEventData` is that check. A review key spelled in the wrong case names the key to rename it to. `api.Handler` takes a failure hook that receives the cause of each internal failure, and a panic in a call is answered as an internal failure. `api.HeaderCaller` is renamed `api.UnauthenticatedHeaderCaller`. The client has a 30-second default timeout and refuses a successful answer over 16 MiB. The conformance suite has 82 cases.

- M1 surface (issues #67, #68, #62, #70): the agent surface is 13 tools plus
  `recall` and `search`. `event_list` folds into `task_get` (`section: events`,
  paged, with `step`), and `review_record`, `note_put` and `note_get` are
  removed from `core.Service`. A review is an `event_log` entry of kind
  `review`, whose data holds the verdict and counts; a missing or malformed
  field is `invalid`, and the message names the fix. CONTRACT §9 item 39 (Bishop
  default, pending operator approval) rules that an agent may log a review. The store
  is unchanged. The task_current snapshot is tested (CONTRACT §9 item 35), and
  an error-text audit fails on any error literal that advises `--force`, `rm` or
  `reset`. The conformance suite has 80 cases.
- Plan v1: central server only, 13 tools plus recall and search, viewer scope, hooks and CLI, dashboard; milestones M1–M8 and Later replace phases 3–10 ([PR #65](https://github.com/djm56/cairn-memory/pull/65)).
- Decision D19 (issue #71): M1 builds no network listener in the shipped binary. `internal/api` lands in M1 as handlers only, and `internal/client` is tested against an in-process server. M1's exit check is an MCP stdio test over that server. The live run against `cairn serve` moves to M2's exit check (#19) ([PR #101](https://github.com/djm56/cairn-memory/pull/101)).
- Phase 1 closed: the operator approved `CONTRACT.md` §9 items 1 to 11 on
  2026-10-28, item 12 is deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)),
  and `DESIGN.md` is aligned with the contract
  ([#39](https://github.com/djm56/cairn-memory/issues/39))
  ([PR #42](https://github.com/djm56/cairn-memory/pull/42)).
- Plans now say Cairn is written fresh, guided by bishop-memory, rather than porting its code ([PR #44](https://github.com/djm56/cairn-memory/pull/44)).
- Phase 2 core, issue #9: review fixes and the rules the contract
  still needed. `task_update` refuses a status the task already has, instead of
  ignoring it. Recall keeps the words of a goal that can name its subject, and
  in a goal written all in capitals it keeps only words of four or more letters
  and words with a digit. A step ref names exactly one positive id or number. An
  operator call needs the operator's name, a directive can be retired once, a
  learning cannot be superseded in a cycle, and project settings must be given
  whole. A name made of spaces counts as no name. The rules are listed in
  CONTRACT §9 as items 13 to 33, and they await the operator's approval. The
  conformance suite has 51 cases, all passing
  ([PR #57](https://github.com/djm56/cairn-memory/pull/57)).
- Phase 2 store, issue #11: a test starts two processes against one database
  file, and each allocates task ids while the other does. Every id is distinct.
  The fold-ins from the review of PR #57: a project's settings must give every
  switch, or the call is refused; a goal in an uncased script keeps its short
  words (two or three characters); a name of only spaces counts as no name (CONTRACT §9 item 34); and
  number 0 is valid only as `step_add`'s `after` (§9 item 19). The conformance
  suite has 63 cases, all passing, and each of CONTRACT §9's items 13 to 34 is
  checked by a conformance case, or is marked `(core test only)` ([PR #58](https://github.com/djm56/cairn-memory/pull/58)).
- Phase 2 exit: the conformance suite passes against the local service (66
  cases, all passing), two processes allocate distinct task ids, and
  `go test -race` is clean. Mutation testing is added with `make mutation`,
  which runs Gremlins v0.5.1 on `internal/core` and `internal/store`; it is
  not part of CI. Core is at 96.9% efficacy after new tests, and store at
  92.6%. Conformance rows now pin the learning status moves, operator directive
  retirement, learning writes on done and failed tasks, and reviews of active
  and done steps. The phase markers and status lines move to phase 3
  ([PR #59](https://github.com/djm56/cairn-memory/pull/59)).
- Phase 2 store, follow-up to issue #46 (task-id day range): `AllocateTaskID` finds the day's
  task ids with a range on `id` instead of `substr(id, 1, 14)`, so the query can
  search the primary-key index. The `CAST` maximum is unchanged ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store, follow-up to issue #46 (project-match trigger): migration 0003 adds the
  `events_project_matches_task` trigger, which refuses an event whose project is
  not its task's project. `InsertEvent` returns the new `ErrProjectMismatch` for
  that refusal. DESIGN §3 names the trigger ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 docs, follow-up to issue #46: DESIGN §3 says that core owns
  the directive scope rule and the non-negative review counts. Core already
  checks both, so no code changed ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store tests, follow-up to issue #46: `loadMigrations` has
  table-driven refusals for a bad file name, a duplicate version and a gap. The
  enum CHECK test covers all 13 CHECK sets. The migration-failure and
  newer-database tests are table-driven ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store tests, follow-up to issue #46 (search note row): the search test asserts the
  project of the note row, and that the task_id of the learning and note rows
  is NULL ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store, follow-up to issue #46: `Open` joins the close error
  to the migrate error instead of dropping it. `Open(":memory:")` limits the
  pool to one connection, so every call sees one database; its docblock says
  so, and a test checks that a write is read back by a later call ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store, follow-up to issue #46 (StepPositionBetween docblock): the `StepPositionBetween` docblock
  says that about 50 inserts at one spot exhaust float64 precision, and that
  step order then falls back to id ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 tests, follow-up to issue #60: each store list function with a limit
  has an exact-fit page test and a limit-one test. Core tests cover the
  `started_at` of a queued task on activation, key files written with a block,
  one-character, `Go` and all-digit words in recall, a review with a negative
  warnings count, and the global directive scope. A conformance case reviews a
  pending step as well as an active and a done one. The `:memory:` test fails
  when the connection limit is removed ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store, follow-up to issue #60: `Open` joins the close error on the
  connect path too. Migration 0003 drops its dead `task_id` guard. CONTRACT §9
  items 18 and 21 say what the tests pin, and item 21 marks only its
  core-only half ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core, follow-up to issue #52: `task_current` reads its task,
  criteria, steps, events and directives in one read transaction, so the answer
  is one consistent snapshot. The store has `WithReadTx`, a deferred read
  transaction that takes no write lock. CONTRACT §9 item 35 ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core, follow-up to issue #52: a `task_update` that moves a
  task from `blocked` to `active` writes an `unblocked` event in the same
  transaction. CONTRACT §9 item 36 ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core, follow-up to issue #52: a `skip` step update sets
  `ended_at` only when the step has none, so a step skipped from `failed` keeps
  the end time of its failure. CONTRACT §9 item 37 ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core, follow-up to issue #52: CONTRACT §1.1 and §9 item 38
  say that a write moves the project's `last_seen_at` on and a read does not.
  The code already behaved so; the `ensureProject` comment now says it, and a
  core test checks it through the store ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 tests, follow-up to issue #60 (conformance rename and recall tests): conformance case
  `event/review_record_on_active_and_done_step` is renamed
  `event/review_record_on_each_step_status`, and it reviews a failed and a
  skipped step as well, so all five step statuses are pinned. The `:memory:`
  test's docblock says its result is set by the pool, not the clock, and it
  closes the rows of its second query. Recall tests drop a single upper-case
  letter and a single CJK character. A queued task's start time is checked
  before activation ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core, follow-up to issue #52 (step_plan refusal): `step_plan` refused over
  pending steps with events or reviews names every such step, not only the
  first. CONTRACT §3 and §9 item 15 say so, and conformance case
  `step/plan_refusal_names_each_recorded_step` pins two. The conformance suite
  has 70 cases ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 tests, follow-up to issue #52, which closes issue #48:
  conformance case `search/hides_retired_directives` ratifies a second, active
  directive with the same word, retires the first, and asserts that search
  returns exactly the active one ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store tests, follow-up to issue #52 (punctuation-only search words):
  `TestSearchIgnoresWordsWithoutLetters` checks the OR form (`Any`) as well as
  the AND form, so a punctuation word cannot empty either search ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 store, follow-up to issue #52 (WithReadTx snapshot test): `TestWithReadTxIsSnapshot`
  pins that a read transaction does not see a write committed while it is open,
  and sees it once it ends. `WithReadTx`'s docblock warns that a call on the
  Store inside fn waits forever on `:memory:`, and DESIGN §3 names it for
  snapshot reads ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core, follow-up to issue #52 (activeTasks docblock): the `activeTasks` docblock
  says it may run inside a read transaction as well as a write one ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 docs, follow-up to issue #52 (CONTRACT header and §9 preamble): the CONTRACT header and
  §9 preamble say that items 1 to 11 are operator-approved and that every item
  from 13 on is a Bishop default, pending operator approval. §9 item 35 is
  marked `(not tested)`, since no test checks `task_current`'s snapshot ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 wiki, follow-up to issue #52 (Getting-Started wording): Getting-Started says the
  conformance tests check the suite against `core.Unimplemented`, and that
  `core.Local` passes all 70 cases. It no longer says the contract fails ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 docs, follow-up to issue #50: DESIGN §2.1 says that a `.cairn.toml`
  which is a directory is refused and names its path, and DESIGN §5.1 says that
  project resolution is built and that agent and session resolution come in a
  later phase. The identity test pins the path. The refusals that name
  `cairn init` already do, so no message changed ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 tests, follow-up to issue #52 (step_plan refusal wording): conformance
  case `step/plan_refusal_names_each_recorded_step` checks that the refusal
  names steps 1 and 3 and not step 2, as whole numbers, instead of core's exact
  wording ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 core tests, follow-up to issue #52 (step_plan refusal wording):
  `TestRecordedRefusal` pins the step_plan refusal for one, two and three
  recorded steps: the singular wording, and the ", " and " and " join ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- Phase 2 docs, follow-up to issue #52 (CONTRACT §9 layout and markers):
  CONTRACT §9 item 15 is reflowed, and item 35's `(not tested)` marker sits on
  its own line, as item 18 places its `(core test only)` marker ([PR #63](https://github.com/djm56/cairn-memory/pull/63)).
- M2 toolchain: `go.mod` pins `toolchain go1.26.10` and keeps `go 1.26.0`, so
  local builds and CI, which reads `go.mod`, use the same Go release ([PR #120](https://github.com/djm56/cairn-memory/pull/120)).
- M2 wire form, issue #109: a request key must be written as its field is named,
  in the same case, and a key given twice is refused at any level (`400`). A
  body with content must be `application/json`, or it is refused (`415`, kind
  `unsupported_media_type`). A path that is no call is `404` with the kind
  `no_route`, and its message names the server base URL to check. A missing
  entity stays `not_found`. DESIGN §5.4 states the versioning rule, and
  CONTRACT §1.2 and §9 items 40 to 42 record the changes ([PR #120](https://github.com/djm56/cairn-memory/pull/120)).
- M2 decisions, issues #78 and #109: D20 (`cairn serve` carries optional TLS
  certificates, and refuses a non-loopback bind without TLS unless
  `--behind-proxy` is given) and D21 (the wire form above, and `step_add` and
  `step_update` keep their assignee field) are recorded in the wiki and in
  PLAN §8 ([PR #120](https://github.com/djm56/cairn-memory/pull/120)).
- M2 step 15, deploy and the live run (issues #75 and #19, [PR #130](https://github.com/djm56/cairn-memory/pull/130)): `docs/DEPLOY.md` and the systemd unit `docs/deploy/cairn.service`, for both TLS setups of D20, with the proxy rules; `docs/M2-LIVE-RUN.md` and `scripts/m2-live-run-check.sh`, for the operator's run on a second machine; the M2 status lines, and the links to DEPLOY.md. The restore fold-ins from step 14b: the raw path says the lock could not be checked; the printed recovery command quotes its paths; the refusal for an interrupted restore names the newest file and lists every stamp; a refusal after the lock says what was and was not changed; a lock that fails after the check and a hot journal each give a next step; and a directory sync that some file systems refuse is tolerated. The symlink race between the backup's create and its copy is open as [issue #129](https://github.com/djm56/cairn-memory/issues/129).
- M2 review fixes ([PR #130](https://github.com/djm56/cairn-memory/pull/130)): DEPLOY.md gives each setup one numbered order for a fresh host (the service user, the state directory and the first key before the unit starts, because a non-loopback server refuses to start with no key and `key add` does not create its directory), and M2-LIVE-RUN.md part 1 follows it for every server variant. New: a route for a server with no public DNS name (an SSH tunnel, or a private CA), a certificate renewal hook, an upgrade rollback that restores the old binary and the pre-upgrade backup together, the unit's checks, and the macOS client steps. README's key recipe works in bash and zsh. The refusal for an interrupted restore offers the newest main file, never a `-wal` or `-shm`, and offers no `mv` when only those remain; the `sqlite3` next steps name the service user and the tool.
- M2 live-run doc fixes from the exit review (issue #19): DEPLOY.md records what the live run observed and not what it expected, and the unit's first systemd start, with no Apache version named; M2-LIVE-RUN.md labels the unit and terminal commands apart, adds the user-scope MCP route and its cleanup, keeps keys out of pasted output, adds a server-side proxy check, and has the 4.2 prompt finish both steps. ([PR #132](https://github.com/djm56/cairn-memory/pull/132))
- M4 (issue #178): the errnos that mean a filesystem cannot make a hard link are listed once, in `internal/hardlink`, which `cairn init` and `cairn backup` both use. Before, each kept its own copy. The backup's hard-link hint is now tested for each errno.
- Behaviour change, M4 (issue #177): a backup refused after its copy was linked reports the real result of removing the output name it made, and removes its staging directory through the directory's root, as its other refusals do. Before, the message said the name was removed whether or not it was, and the staging directory was left behind. Where the staging name itself was swapped, the directory is not removed by name; the refusal says so.
- Behaviour change, M4 (issue #176): `cairn restore` of a database file that cannot be read as SQLite reads the file's SQLite lock bytes with a read-only probe, built for Linux, macOS, FreeBSD and OpenBSD, and refuses, with nothing moved, when another process holds them. The probe's tests run on Linux (CI) and macOS; FreeBSD and OpenBSD are built and vetted, not run. The probe runs before the prompt and again just before the first move, so a holder that starts while the path is typed is refused too. The probe changes no file and takes no lock. It cannot see every holder: an idle connection in rollback-journal mode holds no lock, so when the probe finds none the prompt still says to stop `cairn serve`. Where it cannot tell, and on other systems, the prompt keeps the warning that the lock could not be checked.
- Behaviour change, M4 (issue #113): `cairn doctor`, `status`, `export` and `hook` print a local error whole, so a long path or a long fix in it is no longer cut. Only a message the server wrote is capped, at 200 characters, as before. Control and format characters are still removed from both.
- Behaviour change, M4 (issue #114): `cairn init` accepts a working directory that is the project directory under another case, on a case-insensitive filesystem such as macOS's default. Before, it refused that directory as outside `CLAUDE_PROJECT_DIR`, with two paths that looked the same. The check now compares the directories on disk, not their names.
- Behaviour change, M4 (issue #111): an MCP tool result over 64 KiB is a tool error, not the whole result. A `task_get` with no section on a long task was the case that reached it. The error names the `section` and `limit` arguments, which narrow the read, and `cursor`, which pages it. A brief over the cap cannot be narrowed by section, so it is refused as well; the cap is a judgement and is in `internal/mcp/result.go`.
