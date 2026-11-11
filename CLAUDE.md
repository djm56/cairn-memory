# CLAUDE.md

Cairn is a memory for coding agents: task state, learnings, directives and
recall, behind MCP tools, in one Go binary (`cairn`) over SQLite.

## Read first

- [docs/PLAN.md](docs/PLAN.md) is the build plan. Work happens in milestone
  order (§2: M1 to M8, then Later), and each milestone ends with the exit check
  in §3. **Check the status line at the top of PLAN.md for the current
  milestone** before starting work. The rule for that line is in
  [PLAN §9](docs/PLAN.md#9-project-management).
- [docs/DESIGN.md](docs/DESIGN.md) is the contract: concepts, data model
  (§3), the MCP tool list (§4), packages (§5.1) and commands (§5.2). Do not
  add tools, tables or commands it does not list without updating it first.
- [docs/CONTRACT.md](docs/CONTRACT.md) is the call-level contract: each
  tool's fields, errors and rules. `internal/core` is its Go form and
  `internal/conformance` checks it; change all three together.
- Adopted decisions are recorded in PLAN.md §8. Do not reopen them without
  the operator. Decisions still pending are listed in
  [wiki/Decisions.md](wiki/Decisions.md).
- [CONTRIBUTING.md](CONTRIBUTING.md) holds the engineering rules. The short
  form: rules live only in `internal/core`; no SQL outside `internal/store`;
  the conformance suite is the gate; tests before code; tool output is small
  because a model reads it; a missing project is an error, never a default.
- **Where status lives.** Status lines and the `cairn-milestone` marker follow
  the rule in [PLAN §9](docs/PLAN.md#9-project-management). Live progress is on
  the milestones and the Projects board.
- **Before you start work,** find or open the issue, and reference it in the
  pull request (`Closes #N`).
- **Every pull request** updates CHANGELOG.md unless it carries the
  `skip-changelog` label, and the wiki page when user-facing behaviour changes.
  See [CONTRIBUTING.md](CONTRIBUTING.md#keeping-docs-current).
- **Agents merge only when the gates pass.** A pull request may be merged by
  an agent only when CI is green and the code review is APPROVED with no open
  CRITICAL finding. Merge with a merge commit, then delete the branch. The
  operator may merge at any time. Agents never run the bootstrap script or the
  wiki publish script with `--apply` unless the operator asks for it
  (`scripts/github/`).

## Progress tracking (GitHub first, PROGRESS.md mirror)

- GitHub milestones, issues and the
  [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)
  are the source of truth for progress.
- [PROGRESS.md](PROGRESS.md) is the offline mirror. A pull request that closes
  an issue, passes an exit check, records a decision or opens a milestone
  updates PROGRESS.md in the same pull request.
- Every milestone is written Quick read first (Goal, Status, Exit check, Next
  action), then the detail. That applies in PROGRESS.md and in the GitHub
  milestone descriptions.
- Before you start work, read PROGRESS.md's Quick read, then confirm it against
  GitHub. If they disagree, GitHub wins and PROGRESS.md is fixed in the same
  pull request.

## Commands

```bash
make build && bin/cairn version
make test        # go test -race ./...
make vet
make fmt-check   # CI fails on unformatted files
```

## Reference implementation

Cairn replaces bishop-memory, which is checked out locally; ask the operator for the path.
PLAN.md §1.1 and each phase's "Guided by" line name the files to read for
reference. Read them there. Write Cairn's code fresh: copy nothing from that
checkout, and never write to it or to its database. Bishop and
mission words are not used in Cairn: it says task, project and agent.

## Conventions

- Module `github.com/djm56/cairn-memory`, Go 1.26 (go.mod pins it for CI).
- Standard library first. A new dependency needs a reason in the PR.
- Docs are written in plain, short English and carry a dated status line.
