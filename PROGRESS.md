# Cairn progress

Offline mirror. GitHub milestones, issues and the Cairn Memory Roadmap board are the source of truth; if this file and GitHub disagree, GitHub wins and this file is fixed.

Last synced: 2026-11-24, from main at d3ff98f, the merge commit of PR #229 (m5-plugin into main, merged 2026-11-21), with the M5 counts already reflecting #235 closing when `m5-guide-post-merge` merges. On that merge PR #229 closed #21 to #26, #193 and #206. Decisions D34 to D37 are on main. Decision comments are posted on #193, #206, #20 and #23.

Live progress: [milestones](https://github.com/djm56/cairn-memory/milestones) and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects). Issue counts come from the issue list, not the milestone API, which counts merged pull requests.

Install and update a test server: [docs/QUICK-INSTALL.md](docs/QUICK-INSTALL.md) and [docs/QUICK-UPDATE.md](docs/QUICK-UPDATE.md).

## Quick read

| Milestone | Status | Exit check | One line |
|---|---|---|---|
| Phase 0 Skeleton | Done | Passed | Go module, `cairn version`, Makefile, CI |
| Phase 1 Contract | Done | Passed | Service interface, CONTRACT.md, conformance suite |
| Phase 2 Core and local store | Done | Passed | SQLite store, migrations, core rules, identity |
| M1 Server-mode MCP | Done | Passed | `cairn mcp` over the client and API |
| M2 Server | Done | Passed 2026-11-02 | `cairn serve`, key scopes, backup and restore |
| M3 Web UI + dashboard | Done | Passed | Web pages, sign-in, viewer mode, dashboard |
| M4 Hooks + CLI | Done | Passed by operator ruling 2026-11-06 | Claude Code hooks, CLI verbs with `--json` |
| M5 Plugin | Current | Not passed | Plugin tree, skill text, CI validate and install guide (#25) merged to main 2026-11-21 (PR #229); live check (#26) done 2026-11-17 for the tool names and session start, not for the marketplace install or PreCompact; the exit check (install guide section 7a) not yet run |
| M6 Import | Planned | Not passed | `cairn import bishop-memory`; no code yet |
| M7 Pilot | Planned | Not passed | Two weeks of real use; operator decisions first |
| M8 Roll-out | Planned | Not passed | Every project moves; bishop-memory goes read-only |
| Later | Later | Opens after M8 | Review loop, per-harness plugins, public releases |

**Now**

- M5 Plugin: merged to main (PR #229, 2026-11-21). The operator runs the exit check from `wiki/Install-Plugin.md` section 7a on a clean machine. Epic #20 stays open until it passes. #235 (install guide fixes) is closed by the post-merge guide PR from `m5-guide-post-merge`, on merge.

**Next**

- M5 carry-over fixes. #218 must be fixed before M7 (operator ruling).
- M6 Import: the command, its mapping (#89) and its counts report (#90), under epic #29.
- M7 and M8 after that, once their operator decisions are in.

**Waiting on the operator**

- Pending operator approval (wiki/Decisions.md:46-48):
  - D27: an operator write to an unknown project is refused.
  - D28: the shipped unit stays without `--behind-proxy`.
  - D29: restore refuses a stray `-wal` or `-shm` instead of moving it aside. The code already does this (`internal/server/restore.go:128-133`), so the approval records shipped behaviour.
- Bishop defaults in docs/CONTRACT.md §9, items 13 onward except item 39 (adopted as D32), are not approved. CONTRACT.md:5-8 says so.
- #91: choose the pilot project and two developers.
- #92: choose a second operator.

## Milestones

### M5 Plugin (current)

- **Goal:** A Claude Code plugin under `plugins/claude-code` (DESIGN §6.1): manifest, `.mcp.json`, `bin/cairn-launch`, `hooks/hooks.json`, a skill, `/cairn:init` and `/cairn:status`, a root marketplace file, `claude plugin validate` in CI, and an install guide with confirmed tool names.
- **Status:** Current. Not passed. 26 issues open, 13 closed (counted from the issue list, with #235 counted as closed, on merge of the post-merge guide PR from `m5-guide-post-merge`). The plugin tree is on main, merged from m5-plugin by PR #229 on 2026-11-21 at d3ff98f: manifest, `.mcp.json`, `bin/cairn-launch`, `hooks/hooks.json`, `skills/cairn/SKILL.md`, `/cairn:init`, `/cairn:status`, the root marketplace file, with tests. The `plugin` job in `ci.yml` runs `claude plugin validate` with Claude Code pinned at 2.1.326. Not released. PR #229 closed #21 to #26, #193 and #206. Epic #20 stays open until the exit check passes.
- **Exit check:** "A new developer on a clean machine installs and records a task in 10 minutes from the guide, with no other instruction file." (PLAN §3)
- **Next action:** The operator runs the exit check from `wiki/Install-Plugin.md` section 7a on a clean machine.

**Detail**

Open issues, grouped (26):

- Decisions (0 open): #193 (the plugin and `cairn enable` are alternatives; D34) and #206 (the hook text stops asking for `task_current`; D35) closed with PR #229 on 2026-11-21. The M4 "not run" rule is unchanged.
- Exit-critical build (1): #20 epic. #21 layout and marketplace, #22 skill text, #23 `cairn-launch`, #24 `claude plugin validate` in CI, #25 install guide and #26 live confirmation of tool names and hook behaviour closed with PR #229 on 2026-11-21.
- Carry-over code and tests (16): #140 (restore: stray `-wal` and `-shm` files; the refusal is already in `internal/server/restore.go:128-133`), #191 (a `task_get` brief over the 64 KiB cap cannot be narrowed), #192, #194, #195, #196, #201, #212, #213, #215, #216, #217, #218 (MCP reads send no caller name, so agent-scoped directives are dropped), #230 (doctor reports plugin-carried hooks ok without reading `hooks/hooks.json`), #231 (PreCompact wording in `enable.go` and doctor), #232 (skill test checks argument names against each tool's InputSchema; restore `learning_add`'s required fields).
- Carry-over docs (9): #116 (init docs), #169 (M3 hand-test guide), #197 (CLI help for exit codes), #200 (`cairn init` help text), #224 Doc drift sweep 2026-11-11 (the drift tracking issue), #225 Quick docs polish (review suggestions S1–S5), #233 (MCP tool examples name the agent `main`; use a placeholder), #234 (`cairn enable --help` plugin-check rule does not match `wiki/CLI-Reference.md`), #239 (plugin guide section 8 removal wording).

Left before the exit check passes:

1. The skill text (#22) is on main, and a test checks it against the tools the server registers. The CI validate job (#24) is on main too. The install guide (#25) is `wiki/Install-Plugin.md`; its tool names are confirmed by the live check (#26, 2026-11-17). The DESIGN §6.1 tree is on main.
2. `/cairn:init` and `/cairn:status` are on main, under #21 and #23. No issue names them on its own. Both issues are closed.
3. Decided 2026-11-17: #193 (D34) and #206 (D35). Both are on main, and both closed with PR #229 on 2026-11-21. The skill text for D35 is in the skill (#22).
4. Live check of #26, done 2026-11-17 on the m5-plugin branch, before the merge, with Claude Code 2.1.311 and the plugin loaded by `--plugin-dir`. Confirmed: the server `plugin:cairn:cairn` and its tool names, the commands, the session-start block with and without a task, a task recorded through the plugin's tools, the rule that a step is started before it is finished, and the second server that a user-scope `cairn` entry adds. Not confirmed: the marketplace install, the PreCompact hook's effect, the launcher's exit 127 display, and what `/mcp` prints. `CLAUDE_PLUGIN_ROOT` and `CLAUDE_PROJECT_DIR` were not isolated: the probe ran in the project folder, so it cannot tell which one resolved the project.
5. Decided 2026-11-17 (D36): the API key lives only in `~/.config/cairn/client.env` or `CAIRN_API_KEY`, and the plugin has no `userConfig`. Recorded on main since PR #229 merged on 2026-11-21.
6. Decided 2026-11-17 (D37): `CAIRN_HOME/bin` is dropped. The launcher searches PATH, then `~/.local/bin`. `.github/roadmap.json` is corrected on main. The GitHub text of epic #20 and of issue #23 no longer names `CAIRN_HOME/bin`, and decision comments are posted on both.
7. No release archives exist, so a clean-machine install means a source build with Go 1.26. The exit check does not say whether that counts. No issue covers the exit run itself.

### M6 Import (planned)

- **Goal:** `cairn import bishop-memory --db <copy> [--dry-run]` reads the SQLite database, never Markdown, follows the mapping in PLAN §5.1, is idempotent, and prints a counts report.
- **Status:** Planned. Not started. 3 issues open, 0 closed. The binary has no `import` verb (wiki/CLI-Reference.md:34 lists it as planned).
- **Exit check:** "Dry run on a copy maps every mission, step, event, finding, directive, criterion, lesson, mistake and deliverable, or names each skip; counts match; history visible on the dashboard." (PLAN §3)
- **Next action:** Settle the grades conflict (below) with the operator. Then file issues for the command, its idempotence and its dry run.

**Detail**

Open issues (3): #29 epic, #89 mapping update (reviewer verdicts to `review` events; `patterns` and `service_records` to learnings kinds), #90 counts report compared to source.

Left before the exit check passes:

1. The command has no code and no child issue; epic #29 describes it.
2. Idempotence and dry-run have no child issue; epic #29 states them.
3. Grades conflict: epic #29 includes grades, while PLAN §5.1 moves them to Later. Operator decision.

### M7 Pilot (planned)

- **Goal:** One pilot project, two developers and one operator, with a daily review of findings and a weekly look at the dashboard with a project manager.
- **Status:** Planned. Not started. 4 issues open, 0 closed. Most of what is left is operator decisions and calendar time.
- **Exit check:** "Two weeks of real tasks; every finding decided within 48 hours; five directives ratified; a project manager can answer 'what is running' from the dashboard alone." (PLAN §3)
- **Next action:** The operator chooses the pilot project and the two developers (#91), and a second operator (#92).

**Detail**

Open issues (4): #91 pilot project and two developers (operator), #92 second operator (operator), #93 daily findings review slot and weekly dashboard look, #94 retrospective.

Left before the exit check passes:

1. Operator decisions #91 and #92.
2. #218 fixed before the pilot starts (operator ruling).
3. Two weeks of real tasks on the calendar.

### M8 Roll-out (planned)

- **Goal:** `.cairn.toml` in every active project, keys issued per role, harnesses move between tasks, and bishop-memory set read-only, then archived.
- **Status:** Planned. Not started. 3 issues open, 0 closed.
- **Exit check:** "No harness writes to bishop-memory; backups verified by a restore to a scratch server." (PLAN §3)
- **Next action:** File an issue for the scratch-server restore drill and write its procedure.

**Detail**

Open issues (3): #95 `.cairn.toml` in every active project, #96 keys per role and a monthly `cairn key list` review, #97 bishop-memory set read-only, then archived, with nightly jobs off.

Left before the exit check passes:

1. No issue exists for the scratch-server restore drill. wiki/Operations.md:87 says to restore a copy to a scratch path on another host, but no procedure is written down. The rollback text in wiki/Upgrade.md:138-154 is not a scratch drill.

### Later (opens after M8)

- **Goal:** Scheduled review loop, per-harness plugins, public releases, Postgres store, documentation ingestion (PLAN §3).
- **Status:** Later. 22 issues open, 0 closed. Not scheduled.
- **Exit check:** "Opened only after M8." (PLAN §3)
- **Next action:** None until M8 closes. The operator decides #98 (rename, D18) and #99 (internal name).

**Detail**

- Epics (3): #30 scheduled review loop, #31 per-harness plugins, #32 public releases.
- Decisions (2): #98 rename to `cairn` (D18), #99 internal name.
- Hygiene (15): #33, #34, #35, #36, #37, #38, #41, #43, #64, #100, #126, #135, #214, #221, #223. Several are misfiled; see Known doc drift.
- Plugin follow-ups (2): #236 (CI plugin job: try `npm install --global --ignore-scripts` for Claude Code), #237 (plugin icon: `claude plugin validate` reports ICON_MISSING).

### Closed milestones (history)

#### Phase 0 Skeleton

- **Goal:** The Go module, `cairn version`, the Makefile and CI.
- **Status:** Closed 2026-10-30, 1 issue. Exit passed.
- **Exit check:** "`make test` passes in CI on an empty suite; `bin/cairn version` prints a version from `git describe`." (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-10-30, 1 issue. Exit passed. Delivered the Go module, `cairn version`, the Makefile and CI.

#### Phase 1 Contract

- **Goal:** The `Service` interface, the request and result types, CONTRACT.md and the conformance suite.
- **Status:** Closed 2026-10-30, 2 issues. Exit passed.
- **Exit check:** "`CONTRACT.md` covers every tool in DESIGN.md §4. The conformance suite compiles and reports every case as failing against the stub. Review of the contract by the operator before phase 2 starts." (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-10-30, 2 issues. Exit passed. CONTRACT §9 items 1 to 11 approved 2026-10-28. Delivered the `Service` interface, CONTRACT.md and the conformance suite.

#### Phase 2 Core and local store

- **Goal:** The SQLite store, migrations, core rules and identity.
- **Status:** Closed 2026-10-30, 12 issues. Exit passed.
- **Exit check:** "The conformance suite passes against the local `Service`. Two processes writing the same database at once allocate distinct task ids (a test). `go test -race` is clean." (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-10-30, 12 issues. Exit passed. Evidence: `TestConformance` in `internal/core/local_test.go:38` passed at close (82 cases then; 106 now) against `core.Local`. Delivered the SQLite store, migrations, core rules and identity.

#### M1 Server-mode MCP

- **Goal:** `cairn mcp` over the client and the API, with init, status, doctor and export task.
- **Status:** Closed 2026-11-03, 15 issues. Exit passed.
- **Exit check:** "An MCP stdio test drives a small task flow through `cairn mcp` → `internal/client` → `internal/api` → `core` against the in-process server; `cairn export task` prints the result. The live Claude Code run against `cairn serve` on another machine is M2's exit check (#19)." (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-11-03, 15 issues. Exit passed. Evidence: the stdio flow test `TestExitCheckMCPFlowThenExport` (`cmd/cairn/export_test.go:312`). Delivered `cairn mcp` over the client and API, and init, status, doctor and export task.

#### M2 Server

- **Goal:** `cairn serve`, key scopes, backup and restore.
- **Status:** Closed 2026-11-02, 11 issues. Exit passed.
- **Exit check:** "Conformance suite passes over HTTP; agent key refused on operator routes; viewer key refused on every write (tests); M1 flow works with `server` set; the live Claude Code run of a small task against `cairn serve` on another machine (#19), and `cairn export task` prints it" (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-11-02, 11 issues. Exit passed. Evidence: live run #19 (`docs/M2-LIVE-RUN.md`), and `TestConformanceOverRealListener` (`internal/server/exit_test.go:314`), which runs the suite over HTTP. Delivered `cairn serve`, key scopes, backup and restore, the directive cap and `review_by`.

#### M3 Web UI + dashboard

- **Goal:** Web pages, sign-in, viewer mode and the dashboard.
- **Status:** Closed 2026-11-06, 33 issues. Exit passed.
- **Exit check:** "A lead sees a proposed finding, accepts it, ratifies a directive, and `task_current` returns it next session; a viewer key sees every page and gets 403 on every action" (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-11-06, 33 issues. Exit passed. Evidence: `TestM3ExitCheck` (`internal/server/ui_actions_test.go:137`). Delivered the web pages, sign-in, viewer mode and the dashboard.

#### M4 Hooks + CLI

- **Goal:** Claude Code hooks, `cairn enable`, `cairn doctor --agent`, and the CLI verbs with `--json`.
- **Status:** Closed 2026-11-06, 22 issues. Exit passed by operator ruling, for the `/compact` check only.
- **Exit check:** "On Claude Code a task started before `/compact` continues correctly after it with no user prompt; an agent with only bash completes and closes a task through the CLI" (PLAN §3)
- **Next action:** None, closed.

**Detail**

Closed 2026-11-06, 22 issues. Exit passed by operator ruling, for the `/compact` check only (`docs/M4-HAND-CHECK.md` §3). Sections 4 and 5 (OpenCode, pi) are not run; #206 was open under M5 and closed 2026-11-21 with PR #229. Shell-only close test: `TestExitCheckAgentWithOnlyBashCompletesAndClosesATask` (`internal/conformance/cliservice/exitcheck_test.go:54`). Delivered the pre-compact and stop hooks, `cairn enable`, `cairn doctor --agent`, and the CLI verbs with `--json`.

#### Phase 9 and Phase 10 (closed, empty)

- **Goal:** None. The two milestones held no issues.
- **Status:** Closed, empty.
- **Exit check:** Not in PLAN §3.
- **Next action:** None here. Retiring the two milestones is for the operator.

**Detail**

Milestones #10 and #11 are closed and hold no issues. They are left over from before Plan v1, which moved their work to Later. Retiring them is proposed; the operator decides, and nothing is changed here.

## Health snapshot (2026-11-11, main 18fc813)

This is a snapshot; re-run the gates for current state.

- Build, vet and test pass across the 16 Go packages (`go build`, `go vet`, `go test ./...`).
- Conformance: 106 of 106 against `core.Local`. Through the CLI, 98 PASS and 8 deferred (D30, D31, D33).
- `scripts/check-docs.sh`, `make fmt-check`, `make scan-invisible` and `make scan-local-paths` pass.
- The repo ships no release binaries and no launchd unit. A Mac runs `cairn serve` by hand, from a binary built from source (wiki/Install-Local.md:48). Release builds are a Later item (`.github/workflows/ci.yml:4`).

## Where each fact lives

- **docs/PLAN.md** owns the milestones, their exit checks and the decisions (§8).
- **GitHub** owns live progress: milestones, issues and the board.
- **PROGRESS.md** is the offline mirror of GitHub's progress.
- **docs/CONTRACT.md** owns the tool facts: fields, errors and rules.
- **The wiki** owns install, upgrade and operations.
- **CHANGELOG.md** owns what changed.

## Known doc drift

Doc drift found on 2026-11-11 is tracked in #224 (Doc drift sweep 2026-11-11). Other open doc issues carry the `area:docs` label: https://github.com/djm56/cairn-memory/issues?q=is%3Aissue+label%3Aarea%3Adocs. The tracker hygiene items (labels, milestones and board membership) are tracked in GitHub too.

## Keeping this file current

- Update it in the same pull request that closes an issue, passes an exit check, records a decision or opens a milestone.
- Refresh "Last synced" with the date and the main commit at the time of writing.
- Copy, never invent. A fact here must already be true on GitHub or in the repo.
- When in doubt, check GitHub and fix this file.
