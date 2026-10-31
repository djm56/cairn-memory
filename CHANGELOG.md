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

- M2 step 3, API keys (issue #110, [PR #122](https://github.com/djm56/cairn-memory/pull/122)): migration 0004 gives `api_keys` one scope per key and `revoked_at`; `internal/auth` makes and hashes keys and resolves a bearer key to its scope; `api.KeyCaller` takes the role from the key, never from `X-Cairn-Role`. A missing, malformed, unknown or revoked key is `401 unauthenticated`, and a viewer key is refused on every write with `403 read_only`. Each route is marked read or write.
- M2 step 5, `cairn serve` (issues #110 and #112, [PR #123](https://github.com/djm56/cairn-memory/pull/123)): the server's listener. `--db` names the SQLite file and is required; `--addr` defaults to `127.0.0.1:8788`. `--tls-cert` and `--tls-key` go together, and `--behind-proxy` accepts a proxy that terminates TLS. Any address other than loopback needs TLS or that flag, and at least one active key. A loopback server with no key starts, and every call is refused until a key exists. `GET /healthz` needs no key. The failure hook logs the cause of each internal failure, and never the header, the body or a key. A viewer's read of a project that does not exist is `not_found` and writes no row (CONTRACT §9 item 44). An operator route refuses an agent key before it reads the body, with `403 operator_only`. A 401 names the Bearer scheme. Migration 0004 is not yet on `main`. It drops and recreates the empty v1 `api_keys` table, and refuses to run if that table holds any row, so no key is dropped unseen.
- M2 step 7, `cairn key add`, `list` and `revoke` ([PR #124](https://github.com/djm56/cairn-memory/pull/124)): API keys made in the server's database. `add` prints the key once, and `list` never shows a hash or a key. The database is created 0600. The client sends `Authorization: Bearer <key>` on every call, from `CAIRN_API_KEY` or from `~/.config/cairn/client.env`, which is refused unless only its owner can read it. `cairn mcp` refuses to start with no key, and `cairn doctor` reports where the key was found. The default port is now 8788, since 8787 is memoryd's. Also: `cairn serve` refuses `:memory:` and `file:` names, warns when `--behind-proxy` serves without TLS, and stops the drain on a second Ctrl-C.

- M2 step 9, exit tests and review fixes ([PR #125](https://github.com/djm56/cairn-memory/pull/125)): the M2 exit checks run against a real `cairn serve` listener. The conformance suite passes over HTTP, an agent key is refused on every operator route, a viewer key is refused on every write route (#73), and the M1 flow runs with `server` set. The client never follows a redirect, so a key is not sent to a second address. The reach guard keeps the subcommand names to their dispatch table. `key revoke` checks its name, and `key add` revokes a key it cannot show and names the database it creates. A `client.env` may begin each line with `export`, and must be a regular file; a symlink is judged by its target. `cairn doctor` warns when a key would go over `http` to a host that is not loopback.

### Changed

- M1 step 10 review fixes and the stdio flow test (issues #14, #15, #16, #18, #66, #69, #70). The source guard checks the non-test code of every package in the module except `internal/store`, `internal/core` and `internal/api`, which are exempt by name with their reasons. It refuses, in those packages, an import of the store or of the test server, a dot import, a listener function or method of `net`, `net/http` and `crypto/tls`, and a reference to an HTTP server. The error-text audit reads `core.Invalid` and its siblings outside core, and the messages `cairn doctor` prints. Server text that the hook, `status`, `export` and `doctor` print, error messages included, is one printable line: line breaks become spaces, control characters (Unicode category Cc) and format characters (Cf, such as bidi overrides, isolates and zero-width characters) are removed, and escape sequences are removed whole. The hook, `status` and `doctor` cap that line at 200 runes, as does an error message of `export`; the single-line fields of an export are not capped. `export` keeps the line breaks of a section and removes its control and format characters. The hook exits 0 after a panic, and its bound is tested against a server that never answers. `cairn init` writes atomically where the filesystem has hard links, falls back to a direct write where it has none, refuses credentials in the server URL, writes where identity reads, and refuses to run from a working directory outside `CLAUDE_PROJECT_DIR`; below a file with the same values, it answers that nothing changed. `cairn status`, `cairn doctor` and the client print a server URL without its user name and password. A session name is checked like the agent and client names. A panic in an MCP tool writes its stack to stderr, and the model gets only the cause. The conformance suite has 87 cases.
- M1 step 8 review fixes (PR #104): an agent or client name with a control character, or a space at either end, is `invalid` on every call, and the client refuses it before it sends (CONTRACT §1.1). A source guard checks the non-test code of `cmd/cairn`, `internal/mcp`, `internal/client` and the other packages it does not exempt by name, and fails on the store and listener forms it names; it also fails on any non-test use of the unauthenticated caller. Results keep `&&`, `->` and `<` unescaped. A panic in an MCP tool is an internal tool error. A refused write with no agent names the `agent` argument. An unresolved project says to restart the session after the fix. The error-text audit reads `internal/identity`. The conformance suite has 86 cases.
- M1 wire form, the step 6 review warnings of PR #103: event data that is not JSON is refused with core's own message, on the client as in process, and is not sent; `core.CheckEventData` is that check. A review key spelled in the wrong case names the key to rename it to. `api.Handler` takes a failure hook that receives the cause of each internal failure, and a panic in a call is answered as an internal failure. `api.HeaderCaller` is renamed `api.UnauthenticatedHeaderCaller`. The client has a 30-second default timeout and refuses a successful answer over 16 MiB. The conformance suite has 82 cases.

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
