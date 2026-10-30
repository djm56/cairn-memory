# How we work

_Status as of 2026-10-30: M1 (Server-mode MCP) is current. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects)._

## The path of a change

1. **Issue.** Open it from one of the forms: bug, documentation, decision or roadmap task. Blank issues are turned off, and the config links here ([issue forms](https://github.com/djm56/cairn-memory/tree/main/.github/ISSUE_TEMPLATE)).
2. **Branch.** Branch from `main`, named for the change.
3. **Pull request.** The [template](https://github.com/djm56/cairn-memory/blob/main/.github/pull_request_template.md) asks for `Closes #N`, the milestone, and what changed and why. Its checklist covers the four local checks, the changelog, the milestone marker, contract changes, the wiki and decisions.
4. **CI.** The `build, vet, fmt, test` job runs on every pull request ([ci.yml](https://github.com/djm56/cairn-memory/blob/main/.github/workflows/ci.yml)). The docs job checks the milestone markers and the changelog. Shellcheck lints the scripts ([docs.yml](https://github.com/djm56/cairn-memory/blob/main/.github/workflows/docs.yml)). On `main`, only `build, vet, fmt, test` is a required check.
5. **Review.** Only CRITICAL findings block a merge. Warnings are recorded as GitHub issues, or folded into the next planned change when they are a line or two ([CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#review-findings)).
6. **Merge.** An agent may merge a pull request once CI is green and the review is APPROVED with no open CRITICAL finding. Use a merge commit, then delete the branch. The operator may merge at any time ([CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#workflow)). Protection on `main` requires a pull request, requires the `build, vet, fmt, test` check to pass and be up to date, requires no approving review, and lets repository admins bypass these rules.

## Labels

The labels are defined in [labels.json](https://github.com/djm56/cairn-memory/blob/main/.github/labels.json). GitHub's default labels are there too.

| Label | Meaning |
|---|---|
| `phase:0` to `phase:10` | Historical: the roadmap phase a piece of work belonged to, before Plan v1 ([PLAN §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-milestones)). |
| `m1` to `m8`, `later` | The milestone the work belongs to ([PLAN §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#milestone-table)). |
| `area:core` | Rules and types: `internal/core` and `internal/identity`. |
| `area:store` | SQLite store, migrations and typed queries: `internal/store`. |
| `area:mcp` | MCP adapter and `cairn mcp`: `internal/mcp`. |
| `area:plugin` | Claude Code plugin and marketplace. |
| `area:server` | HTTP server and API keys: `cairn serve`. |
| `area:ui` | Web UI pages embedded in the binary. |
| `area:migration` | Import from bishop-memory. |
| `area:review` | Triage and grading loop. |
| `area:clients` | Other clients: OpenCode and pi. |
| `area:release` | Release archives, the Homebrew tap and plugin tags. |
| `area:docs` | Documentation, the wiki and status lines. |
| `area:github` | Repository, CI, workflows and project management. |
| `type:epic` | Milestone summary: the work, the exit check and the child issues. |
| `type:task` | One unit of work from a milestone. |
| `type:decision` | A design decision for the operator ([PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions)). |
| `type:chore` | Maintenance that is not roadmap work. |
| `needs-operator` | Waiting on the operator. |
| `blocked` | Cannot move until something else is done. |
| `skip-changelog` | The pull request does not need a `CHANGELOG.md` entry. |

## Milestones, epics and tasks

- Each milestone (M1 to M8, and Later) is a GitHub milestone.
- A milestone's epic, a `type:epic` issue, holds its work, its exit check and a list of its child issues.
- M4, M7 and M8 have no epic yet. Their work is in [roadmap.json](https://github.com/djm56/cairn-memory/blob/main/.github/roadmap.json) as task issues, and the epics are still to be added.
- Each piece of work is a `type:task` issue. The roadmap task form asks for the milestone, the area, what to build and an acceptance check a reviewer can run.

Roadmap issues are created from [roadmap.json](https://github.com/djm56/cairn-memory/blob/main/.github/roadmap.json).

## The board

The Projects board is named **Cairn Memory Roadmap**. It has three single-select fields of its own:

- **Status:** Todo, In progress, In review, Done.
- **Phase:** 0 to 10, for work from before Plan v1.
- **Area:** the `area:` labels.

The milestone is GitHub's built-in **Milestone** field, not a custom one. Group the table view by Milestone.

Issues opened from the forms are not read or changed by the bootstrap script. The maintainer sets their labels, milestone and board fields by hand.

## Keep these current

- **Milestone marker.** Each of [README.md](https://github.com/djm56/cairn-memory/blob/main/README.md) and [docs/PLAN.md](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md) carries one `<!-- cairn-milestone: M -->` marker, and both values match. CI checks this ([check-docs.sh](https://github.com/djm56/cairn-memory/blob/main/scripts/check-docs.sh)). The rule for when the marker and the status lines change is in [PLAN §9](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#9-project-management), and the checklist is in [CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#keeping-docs-current).
- **Changelog.** Every pull request adds an entry to [CHANGELOG.md](https://github.com/djm56/cairn-memory/blob/main/CHANGELOG.md) under Unreleased, unless it carries the `skip-changelog` label.
- **Contract changes.** DESIGN.md, CONTRACT.md, `internal/core` and `internal/conformance` change together, in one pull request. The rule is in [CLAUDE.md](https://github.com/djm56/cairn-memory/blob/main/CLAUDE.md) and in the [pull request template](https://github.com/djm56/cairn-memory/blob/main/.github/pull_request_template.md). A change that departs from DESIGN.md or PLAN.md updates them in the same pull request, with the date and the reason ([CONTRIBUTING.md](https://github.com/djm56/cairn-memory/blob/main/CONTRIBUTING.md#changes-to-the-design)).
- **Decisions.** A decision that departs from [PLAN §8](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#8-decisions) is raised with the operator first.
- **Wiki.** A pull request that changes user-facing behaviour updates the wiki page. Edit the wiki in `wiki/` by pull request. The pages are published to the GitHub wiki when a change lands on `main` ([wiki.yml](https://github.com/djm56/cairn-memory/blob/main/.github/workflows/wiki.yml)). The publish makes the wiki match `wiki/` exactly. A page edited directly on the GitHub wiki is overwritten, and a page that is not in `wiki/` is deleted ([publish-wiki.sh](https://github.com/djm56/cairn-memory/blob/main/scripts/github/publish-wiki.sh)).

## Bootstrap script

[scripts/github/bootstrap.sh](https://github.com/djm56/cairn-memory/blob/main/scripts/github/bootstrap.sh) creates the labels, milestones, roadmap issues and board from `labels.json` and `roadmap.json`.

- It is a dry run by default. It writes nothing until it runs with `--apply`.
- `--only` limits it to one or more sections: `labels`, `milestones`, `issues` or `project`.
- Re-running it fills only what is missing. It never changes a value a person has set. It deletes, closes and reopens nothing.
- It matches roadmap issues by their marker line, `<!-- cairn-roadmap: KEY -->`.
- It needs `gh`, logged in, and `jq`. The project section also needs the project scope: `gh auth refresh -s project,read:project`.

The script header holds the full contract for each section.
