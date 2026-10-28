# CLAUDE.md

Cairn is a memory for coding agents: task state, learnings, directives and
recall, behind MCP tools, in one Go binary (`cairn`) over SQLite.

## Read first

- [docs/PLAN.md](docs/PLAN.md) is the build plan. Work happens in its phase
  order (§2), and each phase ends with the exit check in §3. **Check the
  status line at the top of PLAN.md for the current phase** before starting
  work. The rule for that line is in [PLAN §9](docs/PLAN.md#9-project-management).
- [docs/DESIGN.md](docs/DESIGN.md) is the contract: concepts, data model
  (§3), the MCP tool list (§4), packages (§5.1) and commands (§5.2). Do not
  add tools, tables or commands it does not list without updating it first.
- [docs/CONTRACT.md](docs/CONTRACT.md) is the call-level contract: each
  tool's fields, errors and rules. `internal/core` is its Go form and
  `internal/conformance` checks it; change all three together.
- Decisions D1–D10 in PLAN.md §8 are **adopted** (2026-10-27). Do not
  reopen them without the operator.
- [CONTRIBUTING.md](CONTRIBUTING.md) holds the engineering rules. The short
  form: rules live only in `internal/core`; no SQL outside `internal/store`;
  the conformance suite is the gate; tests before code; tool output is small
  because a model reads it; a missing project is an error, never a default.
- **Where status lives.** Status lines and the `cairn-phase` marker follow the
  rule in [PLAN §9](docs/PLAN.md#9-project-management). Live progress is on the
  milestones and the Projects board.
- **Before you start work,** find or open the issue, and reference it in the
  pull request (`Closes #N`).
- **Every pull request** updates CHANGELOG.md unless it carries the
  `skip-changelog` label, and the wiki page when user-facing behaviour changes.
  See [CONTRIBUTING.md](CONTRIBUTING.md#keeping-docs-current).
- **Agents never merge.** The operator merges into `main`. Agents never run
  the bootstrap script or the wiki publish script with `--apply` unless the
  operator asks for it (`scripts/github/`).

## Commands

```bash
make build && bin/cairn version
make test        # go test -race ./...
make vet
make fmt-check   # CI fails on unformatted files
```

## Reference implementation

Cairn replaces bishop-memory, checked out at `/Users/djm56/bishop-memory`.
PLAN.md §1.1 and each phase's "Guided by" line name the files to read for
reference. Read them there. Write Cairn's code fresh: copy nothing from that
checkout, and never write to it or to its database. Bishop and
mission words are not used in Cairn: it says task, project and agent.

## Conventions

- Module `github.com/djm56/cairn-memory`, Go 1.25 (go.mod pins it for CI).
- Standard library first. A new dependency needs a reason in the PR.
- Docs are written in plain, short English and carry a dated status line.
