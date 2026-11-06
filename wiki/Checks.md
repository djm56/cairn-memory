# Checks

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da, and against the output of each script as it was run for this page._

This page lists the checks you can run yourself, what each one proves, what it changes, and when to run it. The checks for an install come first. The checks for the repository, and the CI workflows that run them, come after.

## At a glance

| Check | Where you run it | What it changes | Use it |
|---|---|---|---|
| `scripts/smoke-check.sh` | The repository, from any folder | Moves a key's last-used time, at most once a minute | After an install or an upgrade, and before you hand a client its key |
| `cairn doctor` | A project folder | Moves a key's last-used time, when a key is found | When a client misbehaves, and after you set one up |
| `cairn doctor --agent claude-code` | A project folder | Creates the project's row on the server, the first time a project is read | After `cairn enable --agent claude-code` |
| `scripts/check-docs.sh` | The repository | Nothing | Before a pull request |
| `make test`, `make vet`, `make fmt-check` | The repository | Nothing (`make fmt` rewrites files) | Before a pull request |
| The CI workflows | GitHub | Nothing, except the wiki publish on `main` | On every pull request and push |

## `scripts/smoke-check.sh`

The operator check of an install. It runs local checks, and, when given a server, server checks. It changes no task or project data and creates no project.

```bash
scripts/smoke-check.sh [--bin PATH] [--server URL]
```

| Option | Meaning |
|---|---|
| `--bin PATH` | The `cairn` binary to check. Without it, the script uses `cairn` from the PATH |
| `--server URL` | The server's base URL, `http://` or `https://`. It turns on the server checks, and sets `CAIRN_SERVER` for `doctor` and `cairn mcp` |
| `--help` | Prints the usage |

The key is read from the environment variable `CAIRN_API_KEY`, and only from there. The script refuses `--key`, in either form, and never prints the key. Pass it like this, so it stays out of your shell history:

```bash
CAIRN_API_KEY="$(sed -n 's/^CAIRN_API_KEY=//p' ~/.config/cairn/client.env)" \
  scripts/smoke-check.sh --server https://cairn.lan:8788
```

Run it from inside the project folder. `doctor` needs one, and the output of a run outside a project is different (see the notes below).

### What each line checks

| Check | Runs when | PASS when | FAIL when | SKIP when |
|---|---|---|---|---|
| `binary` | Always | The binary is found | No binary at the path given | Never |
| `version` | The binary is found | It prints `cairn <version>` | It prints something else | No binary |
| `doctor` | The binary lists a `doctor` verb | `cairn doctor` exits 0 | It exits 1, and the failing lines are shown | No binary, or no `doctor` verb |
| `healthz` | `--server` is given | `GET /healthz` answers 200 with `{"status":"ok"}` | Another answer | No `--server` |
| `refused` | `--server` is given | `POST /v1/agent/task_current` with no key answers 401 `unauthenticated` | Any other answer, or no answer | No `--server` |
| `key` | `--server` is given and `CAIRN_API_KEY` is set | The same call with the key answers 400 with kind `invalid`. The call names no project, so the server refuses it before it reads anything. That answer shows the key was accepted | 401 or 403, or another answer. A value that is not a Cairn key is refused before it is sent | `CAIRN_API_KEY` is not set |
| `mcp` | `--server` is given and `CAIRN_API_KEY` is set | `cairn mcp` answers `initialize` and `tools/list`, with the `cairn` server name and a tools list | `cairn mcp` does not answer as expected | `CAIRN_API_KEY` is not set, since `cairn mcp` will not start without a key |

The summary line reads `smoke-check: N passed, N failed, N skipped`.

**Exit codes:** 0 when no check failed, 1 when one did, 2 for a usage error.

**What it writes.** Nothing to the project. The `key` check makes one authenticated call. The server accepts it, and sets that key's last-used time, at most once a minute. The `mcp` check makes no server call.

**You should see**, for a healthy install, seven `PASS` lines and the summary:

```text
PASS  binary   found at /usr/local/bin/cairn
PASS  version  cairn 8b651da
PASS  doctor   exit 0: no check failed
PASS  healthz  GET /healthz answered 200 with status ok
PASS  refused  POST /v1/agent/task_current with no key answered 401 unauthenticated
PASS  key      the key was accepted: the call named no project, so the server refused it before any read (400, kind invalid)
PASS  mcp      cairn mcp answered initialize and tools/list
smoke-check: 7 passed, 0 failed, 0 skipped
```

Without `--server`, the four server lines are `SKIP`, and the summary counts them as skipped.

### Notes that matter

- **The key comes from `CAIRN_API_KEY` for the script's own checks.** The `key` and `mcp` checks read that variable only. `cairn doctor` and `cairn mcp` also read `~/.config/cairn/client.env` when `CAIRN_API_KEY` is not set. So a run without the variable can show `doctor` as `PASS`, because doctor found the key in the file, and `key` and `mcp` as `SKIP`, because the script did not look there.
- **`doctor` needs a project.** It finds one from `CAIRN_PROJECT`, or the nearest `.cairn.toml`, searched upward from `CLAUDE_PROJECT_DIR` when it is set and from the working directory otherwise. Run outside a project, its `project` line fails, and so the `doctor` line is `FAIL`, even when the binary, the server and the key are sound. The detail names the failing line. A broken `.cairn.toml` fails the same way.
- **`doctor` without `--server` goes to the server its configuration names.** With `--server`, the script sets `CAIRN_SERVER`, so `doctor` checks the server you named. Without it, `doctor`'s reach call goes to the server from `CAIRN_SERVER` or the `server` line of `.cairn.toml`, and the key is sent there. Give `--server` to make the run check one server.
- **Without a key, `doctor` fails.** A run with no key anywhere shows `doctor` as `FAIL` (key missing), while the `refused` check still passes. The `doctor` line then names the key, not the server.

### Known gaps

These are recorded for the next change to the script. They are not hidden here.

- The `key` and `mcp` `SKIP` lines say `CAIRN_API_KEY` is not set. That is wrong when `~/.config/cairn/client.env` holds a key, which `doctor` and `cairn mcp` then use. The skip is still correct for the script's own check.
- A `doctor` `FAIL` can be for the project, the key or the server. Its detail names every failing line, but the help and the header speak of the project alone.
- The `mcp` check waits a fixed one second for the replies, so a slow host can fail it.

## `cairn doctor`

Checks the binary, the project file, the server and the connection. It changes no project data.

```bash
cairn doctor
cairn doctor --agent claude-code
cairn doctor --agent opencode
cairn doctor --agent pi
```

Without `--agent`, it prints the lines in [Install-Local](Install-Local#6-check-it): `binary`, `project`, `server`, `key` and `reach`. A `skip` line, such as `reach` with no server, is not a failure.

With `--agent`, it also checks a client:

- `claude-code` lists the Cairn hooks in the Claude Code settings files, and runs each hook once against the server. It warns when a hook is missing, and tells you to run `cairn enable --agent claude-code`.
- `opencode` and `pi` report whether the registration and the instructions `enable` writes are in place. Both are EXPERIMENTAL.

Exit 0 when no line fails, 1 when one does.

## `scripts/check-docs.sh`

Checks two things about the documentation, and nothing else.

```bash
scripts/check-docs.sh [--root DIR] [--base SHA] [--labels LIST]
```

1. **The milestone marker.** `README.md` and `docs/PLAN.md` each carry one `<!-- cairn-milestone: M -->` marker, and the two values match. M is M1 to M8, or Later.
2. **The changelog.** On a pull request with a base commit, `CHANGELOG.md` must be in the change, unless the pull request has the `skip-changelog` label. Without `--base`, this check is skipped.

`--base` and `--labels` default to `PR_BASE_SHA` and `PR_LABELS`, which the docs workflow sets. Exit 0 when every check passes, 1 when one fails.

It does not check the wiki, and it does not check the documentation against the code. The drift tests below do that for two pages.

**You should see**, on a checkout with no pull request:

```text
milestone marker: README.md says M5
milestone marker: docs/PLAN.md says M5
changelog: skipped (no pull request base commit)
docs check: passed
```

## The drift tests

Two Go tests check that the wiki's tables list what the code really has:

| Test | Package | Checks |
|---|---|---|
| `TestMCPToolsPageMatchesRegisteredTools` | `internal/mcp` | The tool names in the marked table of [MCP-Tools](MCP-Tools) are the 15 names the server registers |
| `TestCLIReferencePageMatchesDispatchTable` | `cmd/cairn` | The verb names in the marked table of [CLI-Reference](CLI-Reference) are the names in the binary's dispatch table |

A tool or a verb added, renamed or removed on one side fails the test until the other side agrees. The package `internal/docscheck` reads the markers. A table row whose first column is not one name in backticks is also a failure.

Run them with:

```bash
go test ./internal/mcp ./cmd/cairn
```

The smoke check's own tests run the script against a stub binary and a stub server, in `internal/smokecheck`. `make test` runs all of these, with the race detector.

## The Make targets to run before a pull request

```bash
make build
make test
make vet
make fmt-check
make scan-invisible
make scan-local-paths
make cross
```

[Binaries](Binaries) describes each target. `make mutation` is slow and is not in CI. `make fmt` rewrites files in place, so run `make fmt-check` to see what it would change.

## The CI workflows

Three workflows run in `.github/workflows/`:

| Workflow | Runs on | Does |
|---|---|---|
| `ci.yml`, named `CI` | Every push to `main`, and every pull request | One job, `build, vet, fmt, test`, on Ubuntu with the Go version from `go.mod`: `make build` and `bin/cairn version`, `make vet`, `make fmt-check`, `make test` (with `-race`), `make cross`, `make scan-invisible` and `make scan-local-paths`. The job's name is the required check on `main` |
| `docs.yml`, named `Docs` | Pull requests: opened, updated, reopened, labelled and unlabelled | `scripts/check-docs.sh` with the base commit and the labels, and shellcheck over every `scripts/**/*.sh` at warning severity |
| `wiki.yml`, named `Wiki` | Pushes to `main` that touch `wiki/**`, and a manual run | `scripts/github/publish-wiki.sh --apply`, in one publish at a time. It runs only on `main` |

### How the wiki is published

`wiki.yml` copies the whole of `wiki/` to the GitHub wiki, with `rsync --delete`. So a new page, and `_Sidebar.md` and `_Footer.md`, go out with the rest. No list needs changing. A page that is in the wiki but not in `wiki/` is deleted. An edit made on the GitHub wiki is overwritten at the next publish.

To see what a publish would change, without publishing, run the script without `--apply`:

```bash
scripts/github/publish-wiki.sh
```

It clones the wiki and lists the pages it would add, update or delete. It commits and pushes nothing.

## The hand-check guides

The guides under `docs/` are human checks for each milestone. Their status, as recorded in the audit of this repository:

| Guide | Status | What still holds |
|---|---|---|
| [M2-LIVE-RUN.md](https://github.com/djm56/cairn-memory/blob/main/docs/M2-LIVE-RUN.md) | Exit check passed on 2026-11-02 (#19) | Its steps match the code. Its `systemd-analyze security` score was recorded by the run and was not re-checked for this page |
| [M3-UI-CHECK.md](https://github.com/djm56/cairn-memory/blob/main/docs/M3-UI-CHECK.md) | An optional hand test. The automated M3 exit test is `TestM3ExitCheck` | The headers, the cookie, sign-in with operator and viewer keys, the refusals and the upgrade section. Issue #169 is partly open: its rollback section still has gaps |
| [M4-HAND-CHECK.md](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) | Section 3, the `/compact` check, was run on 2026-11-06 and passed by operator ruling. Sections 4 and 5, for opencode and pi, are not run | Its setup steps and the close steps. Two lines are out of date: section 3.1 says the dry run shows the `compact` matcher, but the dry run lists event names only, and the matcher is written to the file. Issue #203 is open on the restart wording and one phrasing |

The M4 guide's own record is the authority for its status. This page repeats it.
