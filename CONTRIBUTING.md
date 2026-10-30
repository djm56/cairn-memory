# Contributing to Cairn

What Cairn is: [docs/DESIGN.md](docs/DESIGN.md). What is built next and how
each phase is checked: [docs/PLAN.md](docs/PLAN.md).

## Build and check

Go 1.26 or later. No C toolchain: SQLite is `modernc.org/sqlite`.

```bash
make build       # bin/cairn, version from git describe
make test        # go test -race ./...
make vet         # go vet ./...
make fmt         # gofmt -w .
make fmt-check   # fails if any file is not gofmt-formatted
```

CI runs build, `vet`, `fmt-check` and `test` on every pull request. Run the
same four before you push.

### Mutation testing

`make mutation` runs [Gremlins](https://github.com/go-gremlins/gremlins) on
`internal/core` and `internal/store`, one package per run. Gremlins is pinned
in the Makefile (`GREMLINS`, v0.5.1), so `go.mod` does not change. Each run
prints its efficacy (killed mutants over killed and lived) and its mutator
coverage. The run is slow, so it is not part of CI and no threshold gates it.
A surviving mutant is either killed by a new test or recorded in the pull
request as equivalent or accepted, with a reason.

## Workflow

1. **Issue.** Open one from a form: bug, documentation, decision or roadmap
   task. Blank issues are off. Before you start work, find the issue or open
   one, and reference it.
2. **Branch.** Branch from `main`, named for the change.
3. **Pull request.** Use the pull request template. It asks for `Closes #N`
   and the phase milestone.
4. **CI.** `build, vet, fmt, test` must pass. The docs job checks the phase
   markers and the changelog, and shellcheck lints the scripts. Only
   `build, vet, fmt, test` is required on `main`.
5. **Review.** See the rule below.
6. **Merge.** An agent may merge a pull request once CI is green and the
  review is APPROVED with no open CRITICAL finding (see the rule below). Use
  a merge commit, then delete the branch. The operator may merge at any time.
  `main` is protected: a pull request is required, and the
  `build, vet, fmt, test` check must pass and be up to date.

### Review findings

Only CRITICAL findings block a merge. Warnings are recorded as GitHub issues,
or folded into the next planned change when they are a line or two.

## Engineering rules

From what went well and badly in bishop-memory (PLAN.md §6):

- **Rules live in `internal/core`.** A rule enforced in a handler, a hook or
  a doctrine file is a rule that a second path will miss. The conformance
  suite (`internal/conformance`) is the gate: a behaviour exists when the
  suite says so.
- **No SQL in handlers.** All SQL lives in `internal/store`, behind typed
  query functions.
- **Every tool result is read by a model.** Keep it small, paged and
  readable. Test the size of `task_current` on a large task.
- **Tests before code for each tool**, through the conformance suite.
- **Mutation testing** on `core` and `store` from phase 2. Phase 2 uses
  Gremlins (`make mutation`), a Bishop default awaiting the operator's
  ratification.
- **No silent defaults for identity.** A missing project is an error that
  says how to fix it (`cairn init`).
- **Docs are dated.** Status lines carry a date, and claims carry evidence.

### Plan v1 rules

Ten rules from Plan v1 (2026-10-30), added to the rules above.

1. **Boring storage.** SQLite stays. A new store passes the conformance suite
   before it is mentioned in docs. A migration never renames or removes its
   source before import succeeds, and always has `--dry-run`.
2. **No destructive advice in error text.** A refusal names only a safe fix. A
   CI test, not yet written, fails on `--force`, `rm`, `reset`, or any `init`
   other than `cairn init`, in an error string.
3. **One source of truth.** Exports are exports; nothing reads an export back
   except `import` on an explicit command.
4. **Hooks remind, tools record.** Every task-loop step the skill asks for has a
   hook that reminds the agent of it after compaction or at stop.
5. **Directives are capped.** 12 per call, ranked, with `review_by`; the UI
   shows the count and the oldest. A rule that keeps being broken becomes a
   hook check, not a longer sentence.
6. **The loop stays cheap.** No scheduled agents in v1; a human decides findings
   daily and the "waiting on you" count stays under 15.
7. **No silent identity.** Missing `project` or `server` is an error that names
   `cairn init`.
8. **Small tool surface.** 13 tools plus the read tools `recall` and `search`
   (15 names), descriptions under 200 characters, every non-obvious affordance
   shown by example in the skill text.
9. **Confirm plugin facts live.** Tool names and `CLAUDE_PROJECT_DIR` behaviour
   are checked in a live session at M5 and on every Claude Code minor release.
10. **Rules live in core.** No rule in a handler, hook or skill that the
    conformance suite does not also check.

## Changes to the design

DESIGN.md and PLAN.md are the contract for this repository. A change that
departs from them updates them in the same pull request, with the date and
the reason. Adopted decisions are listed in PLAN.md §8.

## Keeping docs current

- **Changelog.** Every pull request adds an entry under `## [Unreleased]` in
  [CHANGELOG.md](CHANGELOG.md), unless it carries the `skip-changelog` label.
- **Phase marker.** README.md and docs/PLAN.md each carry one
  `<!-- cairn-phase: N -->` marker, and the two N values match.
  `scripts/check-docs.sh` checks that they agree. When N and the status lines
  change is set out in [PLAN §9](docs/PLAN.md#9-project-management).
- **Contract changes.** DESIGN.md, CONTRACT.md, `internal/core` and
  `internal/conformance` change together. See
  [Changes to the design](#changes-to-the-design) above.
- **Wiki.** Edit pages in `wiki/` by pull request. The wiki publishes from
  `wiki/` when a change lands on `main`. A page edited on the GitHub wiki is
  overwritten by the next publish.
