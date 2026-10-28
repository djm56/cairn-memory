# Contributing to Cairn

What Cairn is: [docs/DESIGN.md](docs/DESIGN.md). What is built next and how
each phase is checked: [docs/PLAN.md](docs/PLAN.md).

## Build and check

Go 1.25 or later. No C toolchain: SQLite is `modernc.org/sqlite`.

```bash
make build       # bin/cairn, version from git describe
make test        # go test -race ./...
make vet         # go vet ./...
make fmt         # gofmt -w .
make fmt-check   # fails if any file is not gofmt-formatted
```

CI runs build, `vet`, `fmt-check` and `test` on every pull request. Run the
same four before you push.

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
6. **Merge.** The operator merges into `main`. Agents open pull requests and
   never merge them. `main` is protected: a pull request is required, and the
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
- **Mutation testing** on `core` and `store` from phase 2. Phase 2 picks the
  approach (the operator's choice), guided by bishop-memory's harness.
- **No silent defaults for identity.** A missing project is an error that
  says how to fix it (`cairn init`).
- **Docs are dated.** Status lines carry a date, and claims carry evidence.

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
