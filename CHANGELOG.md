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

### Changed

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
  words; a name of only spaces counts as no name (CONTRACT §9 item 34); and
  number 0 is valid only as `step_add`'s `after` (§9 item 19). The conformance
  suite has 63 cases, all passing, and each of CONTRACT §9's items 13 to 34 is
  checked by a conformance case, or is marked `(core test only)` ([PR #58](https://github.com/djm56/cairn-memory/pull/58)).
- Phase 2 exit: the conformance suite passes against the local service (66
  cases, all passing), two processes allocate distinct task ids, and
  `go test -race` is clean. Mutation testing is added with `make mutation`,
  which runs Gremlins v0.5.1 on `internal/core` and `internal/store`; it is
  not part of CI. Core is at 96.9% efficacy after new tests, and store at
  92.6%. Conformance rows now pin the status moves, operator directive
  retirement, learning writes on done and failed tasks, and reviews of active
  and done steps. The phase markers and status lines move to phase 3
  ([PR #59](https://github.com/djm56/cairn-memory/pull/59)).
- Phase 2 store, follow-up to issue #46 (S1): `AllocateTaskID` finds the day's
  task ids with a range on `id` instead of `substr(id, 1, 14)`, so the query can
  search the primary-key index. The `CAST` maximum is unchanged.
- Phase 2 store, follow-up to issue #46 (S3): migration 0003 adds the
  `events_project_matches_task` trigger, which refuses an event whose project is
  not its task's project. `InsertEvent` returns the new `ErrProjectMismatch` for
  that refusal. DESIGN §3 names the trigger.
- Phase 2 docs, follow-up to issue #46 (item 1): DESIGN §3 says that core owns
  the directive scope rule and the non-negative review counts. Core already
  checks both, so no code changed.
- Phase 2 store tests, follow-up to issue #46 (item 2): `loadMigrations` has
  table-driven refusals for a bad file name, a duplicate version and a gap. The
  enum CHECK test covers all 13 CHECK sets. The migration-failure and
  newer-database tests are table-driven.
- Phase 2 store tests, follow-up to issue #46 (S6): the search test asserts the
  project of the note row, and that the task_id of the learning and note rows
  is NULL.
- Phase 2 store, follow-up to issue #46 (item 3): `Open` joins the close error
  to the migrate error instead of dropping it. `Open(":memory:")` limits the
  pool to one connection, so every call sees one database; its docblock says
  so, and a test checks that a write is read back by a later call.
