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
- Phase 2 identity: `internal/identity` resolves the project from
  `CAIRN_PROJECT`, then the nearest `.cairn.toml` walking up from
  `CLAUDE_PROJECT_DIR` or the working directory, and refuses a call with no
  project. An invalid name is refused, not skipped. The file is read with
  `github.com/BurntSushi/toml` v1.6.0 ([PR #49](https://github.com/djm56/cairn-memory/pull/49)).

### Changed

- Phase 1 closed: the operator approved `CONTRACT.md` §9 items 1 to 11 on
  2026-10-28, item 12 is deferred to phase 8 ([#41](https://github.com/djm56/cairn-memory/issues/41)),
  and `DESIGN.md` is aligned with the contract
  ([#39](https://github.com/djm56/cairn-memory/issues/39))
  ([PR #42](https://github.com/djm56/cairn-memory/pull/42)).
- Plans now say Cairn is written fresh, guided by bishop-memory, rather than porting its code ([PR #44](https://github.com/djm56/cairn-memory/pull/44)).
