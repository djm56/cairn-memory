Closes #

Milestone: set it to the phase this pull request belongs to (docs/PLAN.md §3).

## What and why

<!-- What changed, and why. Name the phase and the PLAN §3 work line where it applies. -->

## Checklist

- [ ] `make build`, `make vet`, `make fmt-check` and `make test` pass locally
- [ ] `CHANGELOG.md` is updated, or the `skip-changelog` label is set
- [ ] If a phase exit check passed: the phase marker and status line in README.md and docs/PLAN.md are updated
- [ ] If the contract changed: DESIGN.md, CONTRACT.md, `internal/core` and `internal/conformance` change together (CLAUDE.md)
- [ ] If user-facing behaviour changed: the wiki page is updated
- [ ] Any decision that departs from docs/PLAN.md §8 is raised with the operator first
