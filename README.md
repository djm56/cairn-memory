# Cairn Memory

An operating record for coding agents. Agents record the task they are on, the steps
they take, what happened and what they learned. The next agent reads that
back before it starts. An operator turns the best lessons into rules that
every agent follows.

A cairn is a pile of stones left on a trail for whoever comes next. Each task
in Cairn leaves the same kind of marker.

<!-- cairn-milestone: M5 -->

> **Status as of 2026-11-06:** M5 (Plugin) is current, and the exit checks for M1 (the MCP stdio flow test), M2 (the live run, #19) and M3 (the web UI exit-check test) have passed. M4's exit check passed by operator ruling on 2026-11-06: the `/compact` hand check, section 3 of [docs/M4-HAND-CHECK.md](docs/M4-HAND-CHECK.md), was run and passed. Sections 4 and 5 of that guide (OpenCode and pi) are not run, and #206 is open. Phases 0 to 2 are done, and their exit checks passed. Live progress is on the
> [milestones](https://github.com/djm56/cairn-memory/milestones)
> and the [Cairn Memory Roadmap board](https://github.com/djm56/cairn-memory/projects).
>
> Read [docs/DESIGN.md](docs/DESIGN.md) for what Cairn is, and
> [docs/PLAN.md](docs/PLAN.md) for the build order. Cairn replaces
> [bishop-memory](https://github.com/djm56/bishop-memory), which keeps
> running until the move is done.

At-a-glance progress (offline): [PROGRESS.md](PROGRESS.md). Quick install and update for a test server: [QUICK-INSTALL](docs/QUICK-INSTALL.md) and [QUICK-UPDATE](docs/QUICK-UPDATE.md).

[![CI](https://github.com/djm56/cairn-memory/actions/workflows/ci.yml/badge.svg)](https://github.com/djm56/cairn-memory/actions/workflows/ci.yml)

## What it does

- **Task state.** The current task, its brief, its plan, its steps and a
  journal. A new session continues where the last one stopped.
- **Learning.** Agents propose findings, patterns and notes about agents.
  The operator accepts or rejects each finding. Accepted findings become
  **directives**: rules agents read before they work, in one project or all
  of them.
- **Recall.** Before a task starts, Cairn returns the directives, lessons and
  similar past tasks that apply.
- **Review.** A web page to follow tasks, decide on findings and ratify
  directives.

It works with any agent harness. Claude Code connects through MCP and hooks; OpenCode,
pi and others connect through the CLI, over the same HTTP API. One binary, `cairn`, is
the MCP server, the CLI, the hook helper and the server. The database is the source of
truth, and the operator decides: agents only propose.

## Quick start

Each step, with what you should see, is on the wiki's
[Quick-Start](https://github.com/djm56/cairn-memory/wiki/Quick-Start) page. In brief, on one
machine, with Go 1.26 or later:

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
make build
mkdir -p ~/.local/bin
install -m 0755 bin/cairn ~/.local/bin/cairn
mkdir -p ~/cairn-data
cairn serve --db ~/cairn-data/cairn.db                              # in a second terminal, left running
cairn key add --db ~/cairn-data/cairn.db --name laptop --scope agent  # copy the key it prints, now
mkdir -p -m 700 ~/.config/cairn && chmod 700 ~/.config/cairn
printf 'API key: ' >&2; read -rs K; echo >&2                         # type the key you copied; it is not echoed
(umask 077 && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
cairn init --project my-project --server http://127.0.0.1:8788      # in your project folder
cairn enable --agent claude-code
claude mcp add cairn -- "$(command -v cairn)" mcp
cairn doctor
```

The block stores the key in `~/.config/cairn/client.env` before `cairn doctor` runs, as the
Quick-Start gives it. `cairn doctor` should show five lines, each `ok`.

`.cairn.toml` is committed to the project. It names the project and the server, and `server` is
required (D11). The API key does not go in this file, and neither does a user name or password in
the server URL.

```toml
project = "acme-api"
server = "https://cairn.lan:8788"
```

Once M5 is done, the Claude Code plugin is meant to replace the hand-made `claude mcp add` line.
The repository and the Go module still carry the name `cairn-memory`. Renaming them to `cairn` is
planned (decision D18), and the rename has not been done.

## Where to read next

Each procedure is on the wiki. The wiki is canonical for how to do a thing; the repository
docs are canonical for the design.

| Page | Read it for |
|---|---|
| [Quick-Start](https://github.com/djm56/cairn-memory/wiki/Quick-Start) | One machine, from a clone to a working client, step by step |
| [Install-Local](https://github.com/djm56/cairn-memory/wiki/Install-Local) | A developer machine: the files, the PATH, the key and the hooks |
| [Install-Server](https://github.com/djm56/cairn-memory/wiki/Install-Server) | A server that other machines reach, under systemd, with TLS or behind a proxy |
| [Upgrade](https://github.com/djm56/cairn-memory/wiki/Upgrade) | Upgrading a server or a local install, the backup before it, and the rollback |
| [Binaries](https://github.com/djm56/cairn-memory/wiki/Binaries) | Where the builds come from, and what the version strings mean |
| [MCP-Tools](https://github.com/djm56/cairn-memory/wiki/MCP-Tools) | The 15 MCP tools: what each takes, returns and refuses |
| [CLI-Reference](https://github.com/djm56/cairn-memory/wiki/CLI-Reference) | Every verb, its flags, its environment variables and its exit codes |
| [Checks](https://github.com/djm56/cairn-memory/wiki/Checks) | `cairn doctor` and the operator check, `scripts/smoke-check.sh` |
| [Operations](https://github.com/djm56/cairn-memory/wiki/Operations) | Keys and roles, backups, logs and certificates for a running server |
| [Troubleshooting](https://github.com/djm56/cairn-memory/wiki/Troubleshooting) | The messages you are likely to see, why, and what to do |

## Using the CLI

The CLI is for an agent with only a shell, or a harness without MCP. Each verb calls
the server over HTTP with the key from `CAIRN_API_KEY` or `~/.config/cairn/client.env`,
and needs `server` in `.cairn.toml` or `CAIRN_SERVER`, as `cairn mcp` does. The verbs
that take `--json` write, on one line, the object the MCP tool returns. The full list of
verbs is in [CLI-Reference](https://github.com/djm56/cairn-memory/wiki/CLI-Reference).
`cairn export project` prints every task of the project as Markdown on stdout; redirect it to a file to keep it.

`--agent` names the calling agent. The write verbs, `task current`, `recall`, `prime`,
`status` and `hook session-start` take it from `CAIRN_AGENT` when the flag is left out.
`status` and the session-start hook use it only to count the directives for that role.
`directive list` does not read `CAIRN_AGENT`, so it lists every role unless `--agent` is
given. A write with no agent name is refused on the CLI with exit 2. A malformed
`CAIRN_AGENT` is refused by every verb with exit 2, reads included.

**Hooks.** `cairn enable --agent claude-code` adds three hooks to the project's
`.claude/settings.local.json`: `session-start`, `pre-compact` and `stop`. It keeps every
other entry, and copies the original file to `cairn/backups/<project>` in the user's
cache directory, outside the project, before it writes. `--dry-run` writes nothing.
`enable` writes hooks only. It does not register the MCP server. That is done by the M5
plugin, or by hand with `claude mcp add cairn -- "$(command -v cairn)" mcp`, which uses
the absolute path of the binary.

In Claude Code, the session-start hook is what is meant to carry a task across `/compact`.
Its matcher names `compact`, so it runs again after compaction, and its output is added
to the agent's context. The M4 hand check of that carry, [docs/M4-HAND-CHECK.md](docs/M4-HAND-CHECK.md)
section 3, was run on 2026-11-06 and passed by operator ruling. The agent also called
`task_current` after the compact, and the hook text asks for that call, which the guide's
rule counts as not run. The conflict is tracked in #206. The pre-compact hook is installed,
and it does nothing in Claude Code. [DESIGN.md §4.1](docs/DESIGN.md#41-session-start) has the
design.

**OpenCode and pi (EXPERIMENTAL).** `cairn enable --agent opencode` and `--agent pi` register
the MCP server in the client's own config and write the task-loop instructions into the
project. Neither client has hooks, and neither path has been confirmed by a real run of the
client. The steps are in [Connecting-Agents](https://github.com/djm56/cairn-memory/wiki/Connecting-Agents).

**Exit codes.** A failed call of the verbs that take `--json` exits with the code of its error
kind, so a script can tell a refusal from a missing row. The table is in
[CLI-Reference](https://github.com/djm56/cairn-memory/wiki/CLI-Reference#exit-codes).

## Running the server

`cairn serve` is the server, and the only listener in the binary. It keeps its data in the
SQLite file named by `--db`, which is created if it does not exist. It listens on loopback at
`127.0.0.1:8788` unless `--addr` says otherwise. An address other than loopback needs TLS, by
`--tls-cert` and `--tls-key`, or `--behind-proxy` when a proxy in front of the server terminates
TLS. It also needs at least one active API key. A loopback server with no key starts, and refuses
every call until one exists. `GET /healthz` answers a liveness check without a key. The bind rules
are in [DESIGN.md §5.3](docs/DESIGN.md#53-the-server). The setup for a server that other machines
reach is in [Install-Server](https://github.com/djm56/cairn-memory/wiki/Install-Server) and
[docs/DEPLOY.md](docs/DEPLOY.md).

### Making keys

```bash
cairn key add    --db ./cairn.db --name ci --scope agent
cairn key list   --db ./cairn.db
cairn key revoke --db ./cairn.db --name ci
```

`--scope` is `agent` for the MCP tools, `operator` for decisions and ratification, or `viewer`
for read-only access. `key add` prints the key once, on stdout. Store it then: nothing shows it
again. The database keeps only its hash. `key add` creates the database when it does not exist,
owner-only, and says so on stderr. `key revoke` ends the key at its next call, and a revoked name
is not reused. `key list` and `key revoke` need a database that exists.

`key add`, `key list` and `key revoke` open the database with the same migrator as `serve`. A
newer binary therefore upgrades an older database when you run any of them, even `key list`,
which only reads. Run key commands with the new binary once you are ready to upgrade, and take the
backup first ([Upgrade](https://github.com/djm56/cairn-memory/wiki/Upgrade)).

### Backup and restore

```bash
cairn backup  --db ./cairn.db ./cairn-backup.db
cairn restore --db ./cairn.db ./cairn-backup.db
```

`backup` writes a consistent copy to a new file, owner-only, while the server runs. It refuses a
file that already exists. `restore` checks the backup first (it opens, passes SQLite's integrity
check, and is not from a newer build). For a database it can read, it refuses while the database is
open. For a file it cannot read, no check for a running server is possible, so stop `cairn serve`
before you confirm, whichever path it takes. It then asks you to type the database path at a
terminal, and there is no flag to skip that. A restore from a pipe or a script is refused.

**Pre-restore files are kept.** A restore never deletes anything. Before it moves the database, it
folds the database's `-wal` into the file and switches the file out of WAL mode, so the database file
alone holds all the state the server last saw. Then it moves the database to `<db>.pre-restore-<UTC
time>`. A `-shm` beside it is moved too, to the same name with `-shm` added, and every move is printed.
A file that cannot be read as a database is moved as it is, with no checkpoint, and the output says so.
Keep the kept files until you have checked the restored server, and delete them yourself when you are
sure.

**If a restore fails.** A restore that stops after its first move says where the database before it is,
and the steps that put it back. The files to put back are the ones the restore printed as moved. To put
them back: stop `cairn serve`, move any file now at `<db>` aside, then run `mv <db>.pre-restore-<UTC
time> <db>`, and for a `-shm` the same with `-shm` added to both names. A server that finds no database at
its path, with a `<db>.pre-restore-*` file beside it, refuses to start and says so: that is an interrupted
restore, and it does not create an empty database over it. Put the files back as above, then start the
server. To start a new, empty database instead, check those files, move every one of them out of the folder
yourself, and start the server again: nothing is deleted. A restore into a path with no database also refuses
while a `-wal` or `-shm` is beside it; move those out by hand, then run the restore again.

The rules and the order of the steps are in [DESIGN.md §5.2](docs/DESIGN.md#52-commands). This section is
the one copy of the restore-failure steps, which DEPLOY.md, DESIGN.md and [Operations](https://github.com/djm56/cairn-memory/wiki/Operations)
refer to.

## Setting up a client

A client sends its key with every call. Give it in the environment, or in a file only you can read.
[Quick-Start](https://github.com/djm56/cairn-memory/wiki/Quick-Start) step 4 and
[Install-Local](https://github.com/djm56/cairn-memory/wiki/Install-Local) give the commands. `CAIRN_API_KEY`
is read first. A `client.env` that any other user can read or write is refused, and the client says
`chmod 600`. The client does not follow a redirect, so the key is never sent to a second address.
`cairn doctor` reports where the key was found, and whether the server accepted it. It warns when a key
would go over plain `http` to a host that is not loopback, and it never prints the key.

## Web UI

`cairn serve` also serves a web UI at `/ui/`, from the same process and address as the API. Open it in a
browser and paste an operator key or a viewer key on the sign-in page. An agent key is refused there. A
viewer key reads every page and is refused every action. Operator keys make the changes. The pages are the
dashboard, tasks, learnings, directives and projects. The design is in
[DESIGN.md §5.5](docs/DESIGN.md#55-the-web-ui). The proxy, cookie and rate-limit rules for a server that
other machines reach are in [docs/DEPLOY.md](docs/DEPLOY.md). A hand check of the UI through a real proxy
is in [docs/M3-UI-CHECK.md](docs/M3-UI-CHECK.md).

## Documentation

| Document | What it covers |
|---|---|
| [The wiki](https://github.com/djm56/cairn-memory/wiki) | The procedures: install, upgrade, operate, and the reference pages. Published from `wiki/` |
| [docs/DESIGN.md](docs/DESIGN.md) | Concepts, data model, the MCP tools, architecture, clients, security |
| [docs/DEPLOY.md](docs/DEPLOY.md) | Running `cairn serve` for other machines: TLS, a proxy, the systemd unit, keys, backup, upgrade |
| [docs/M2-LIVE-RUN.md](docs/M2-LIVE-RUN.md) | The M2 live run on a second machine: the checklist and what to report back |
| [docs/M4-HAND-CHECK.md](docs/M4-HAND-CHECK.md) | The M4 hand check: the `/compact` check on Claude Code, and the OpenCode and pi checks |
| [docs/CONTRACT.md](docs/CONTRACT.md) | Every tool's inputs, outputs, errors and rules; the operator calls |
| [docs/PLAN.md](docs/PLAN.md) | Build order: milestones M1 to M8 and Later with exit checks, what is guided by bishop-memory, migration, decisions |
| [CHANGELOG.md](CHANGELOG.md) | What changed, by pull request, under Unreleased |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Building, the checks CI runs, the review rule, and keeping the docs current |

## Project management

- [Wiki](https://github.com/djm56/cairn-memory/wiki): summaries and how the work is run.
- [Milestones](https://github.com/djm56/cairn-memory/milestones): one per milestone, M1 to M8 and Later.
- [Issues](https://github.com/djm56/cairn-memory/issues): the work, from forms.
- [Projects](https://github.com/djm56/cairn-memory/projects): the board that tracks each issue.
- [CHANGELOG.md](CHANGELOG.md): what each pull request changed.

## Licence

MIT, as bishop-memory. See [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
