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
- **Mutation testing** on `core` and `store` from phase 2, with the harness
  ported from bishop-memory.
- **No silent defaults for identity.** A missing project is an error that
  says how to fix it (`cairn init`).
- **Docs are dated.** Status lines carry a date, and claims carry evidence.

## Changes to the design

DESIGN.md and PLAN.md are the contract for this repository. A change that
departs from them updates them in the same pull request, with the date and
the reason. Adopted decisions are listed in PLAN.md §8.
