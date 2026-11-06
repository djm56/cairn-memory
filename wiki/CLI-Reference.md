# CLI reference

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da, and against the help text the binary prints._

The verbs of the `cairn` binary, one line each, as `cairn help` lists them. The sections below give each verb's synopsis, its flags, its environment and its exit codes. The flags are the ones `cairn <verb> --help` prints. Where the help is silent, this page says so.

<!-- The table between the markers is read by cmd/cairn/docs_drift_test.go. Keep the markers, and keep the first column as one verb name in backticks. -->
<!-- cairn-cli-verbs:begin -->
| Verb | Summary |
|------|---------|
| `version` | Print the cairn version |
| `help` | List the sub-commands |
| `mcp` | Serve the MCP tools over stdio, through the Cairn server |
| `init` | Write .cairn.toml: the project name and the server |
| `export` | Print a task, or the project's tasks, as Markdown: export task [id] or export project |
| `hook` | Output for a client hook: session-start, pre-compact or stop |
| `enable` | Install a client's hooks into the project: enable --agent claude-code [--dry-run] |
| `status` | Show the server, the project and the active task |
| `task` | Read or write a task: task current\|get\|list\|start\|update\|close, each with --json |
| `step` | Add, update or plan the steps of a task: step add\|update\|plan, each with --json |
| `event` | Log an event on a task: event log, with --json |
| `learning` | Propose a learning or list them: learning add\|list, each with --json |
| `directive` | List the directives that apply: directive list, with --json |
| `recall` | What applies to a goal: recall --goal text [--agent role], with --json |
| `search` | Full-text search of tasks, events, learnings, directives and notes: search --query text, with --json |
| `prime` | The session-start view for an agent with only a shell: prime [--agent role], with --json |
| `doctor` | Check the project file, the server and the connection; changes nothing |
| `serve` | Run the Cairn server: serve --db <file> [--addr host:port] |
| `key` | Make, list and revoke API keys: key add\|list\|revoke --db <file> |
| `backup` | Copy the server's database to a new file: backup --db <file> <out> |
| `restore` | Replace the server's database from a backup, after a typed confirmation: restore --db <file> <backup> |
<!-- cairn-cli-verbs:end -->

The table has 21 rows, one for each entry in the binary's dispatch table: 19 verbs, plus `help` and `version`. `import` is planned for M6 and is not a verb yet: `cairn import` is an unknown command.

## Conventions

- **Usage.** `cairn <verb> [subcommand] [flags]`. A verb group such as `task` or `step` takes no `--help` of its own. Its subcommand does: `cairn task get --help`.
- **Help.** `cairn help`, `cairn -h`, `cairn -help` and `cairn --help` print the list of verbs and the exit-code table, to stdout, and exit 0. With no arguments, the same text goes to stderr and the exit is 2.
- **Unknown command.** A message on stderr, the list of verbs, and exit 2.
- **`--agent`.** On the verbs that take it, it names the caller. When it is left out, `CAIRN_AGENT` is used. `directive list` does not read `CAIRN_AGENT`: it lists every role unless `--agent` is given. A write needs an agent name. On the CLI, a write with no name is refused before any call, with exit 2 and the message on stderr: `<verb>: no agent name: give --agent <name>, or set CAIRN_AGENT to one`. Over MCP the same case is an `invalid` refusal (exit 3 where a verb reports it), and the adapter asks for the `agent` argument instead.
- **Text from standard input.** A text flag given the value `-` reads standard input, for example `--summary -`. `task start`, `task update`, `task close`, `step plan`, `step add` and `search` take their long text this way.
- **JSON from standard input.** `--json-input` reads one JSON object from standard input. It is on `task start`, `task update`, `task close` and `step plan`. A list left out of the object is absent. On `task update`, a list given replaces the whole list.
- **Lists.** A list with nothing in it prints `no <items>`, such as `no tasks`, `no directives` or `no learnings`. That is not an error, and the exit is 0.
- **Paging.** Verbs with `--cursor` take the value that a previous page printed as its next cursor. Page sizes are in each verb's section.

### Environment variables

These are the environment variables the verbs read directly. [Install-Local](Install-Local) explains how to set them.

| Variable | Read by | Meaning |
|---|---|---|
| `CAIRN_PROJECT` | Every project-scoped verb | The project name. It wins over `.cairn.toml` |
| `CAIRN_SERVER` | Every verb that calls the server | The server's base URL. It wins over the `server` line of `.cairn.toml` |
| `CAIRN_API_KEY` | Every verb that calls the server | The API key. Read before `~/.config/cairn/client.env` |
| `CAIRN_AGENT` | `task current`, `recall`, `prime`, `status`, `hook session-start`, and every write verb | The caller's name, when `--agent` is left out. `status` and the session-start hook use it only to count the directives for that role. A value with a control character, or a space at either end, is refused by every verb with exit 2, reads included |
| `CLAUDE_PROJECT_DIR` | Any verb that looks for `.cairn.toml`, and `init` | The directory to search from, and where `init` writes `.cairn.toml` |

Two more are read indirectly. `HOME` gives the path of `~/.config/cairn/client.env`, and the cache folder of `enable`'s backups on macOS. `XDG_CACHE_HOME`, on Linux, moves that cache folder: `enable` uses it when it is set, and `~/.cache` when it is not. `XDG_CONFIG_HOME` is not read.

### Exit codes

The verbs that take `--json` exit with the code of the error kind of a failed call. A script can tell a refusal from a missing row.

| Exit | Kind | Meaning |
|---|---|---|
| 0 | none | Success |
| 1 | none, or `internal` | A failure with no kind: an internal failure of the server, or one raised on this machine. A missing, malformed, unknown or revoked key (401) also exits 1, and so does a viewer's write (403 `read_only`). Both reach the CLI as plain errors (issue #195) |
| 2 | none | A usage error, refused before any call. The message is on stderr |
| 3 | `invalid` | The request is malformed, a value is unknown, or there is no project. A missing agent name on the CLI is exit 2, not this |
| 4 | `not_found` | The task, step, learning or directive is not in the caller's project |
| 5 | `refused` | A rule forbids the call now: a status move, the close policy, a second active task, or a change to a closed task |
| 6 | `operator_only` | An operator call without the operator role |
| 7 | `no_route` | The path is not a call. The server URL is probably wrong |
| 8 | `unsupported_media_type` | The request body is not `application/json` |

The codes 3 to 8 apply to the verbs `task`, `step`, `event`, `learning`, `directive`, `recall`, `search` and `prime`. The other verbs exit 1 on a failure and 2 on a usage error. `doctor` exits 0 when no line fails and 1 when one does. `serve` exits 0 when a signal stops it, and 1 when it cannot start or stop.

Example, from a shell, with a task whose close policy is not yet met:

```bash
cairn task close task-20261106-01 --outcome done --json --agent main
echo $?
```

A close is a write, so it needs the caller's name: `--agent main` here, or `CAIRN_AGENT=main` in the environment. Without one the command exits 2 and prints the usage message on stderr, with no JSON.

**You should see** `{"error":{"kind":"refused","message":"close policy not met: summary, lessons, mistakes","missing":["summary","lessons","mistakes"]}}` on stdout, nothing on stderr, and then `5`.

### `--json`

- With `--json`, a successful call prints the result as one line of JSON on stdout. It is the object the MCP tool returns, so the shape is the one in [MCP-Tools](MCP-Tools).
- With `--json`, a failed call prints `{"error":{"kind":"...","message":"...","missing":[...]}}` on stdout, and nothing on stderr. `missing` appears only for a refused close.
- A usage error (exit 2) prints its message on stderr, with no JSON, even when `--json` is given. Issue #197 asks for this to be stated in the help.
- Without `--json`, the output is a short text form, not JSON.

## Verbs

### `version`

```bash
cairn version
```

Prints `cairn <version>`. `cairn -v` and `cairn --version` do the same. See [Binaries](Binaries) for the version strings.

### `help`

```bash
cairn help
```

Prints the list of verbs and the exit-code table. Exit 0.

### `mcp`

```bash
cairn mcp
```

Serves the MCP tools over stdio. It is started by an MCP client, not by hand. It takes no arguments: `cairn mcp --help` is a usage error, exit 2.

It needs a server (`CAIRN_SERVER` or `.cairn.toml`) and a key (`CAIRN_API_KEY` or `client.env`). Without either it refuses to start and exits 1, with `invalid: no server` or `invalid: no API key`. With a server and a key but no project, it starts, and every call is refused with the reason.

The tools are in [MCP-Tools](MCP-Tools).

### `init`

```bash
cairn init --project <name> --server <url>
```

| Flag | Required | Meaning |
|---|---|---|
| `--project <name>` | yes | The project name, such as `acme-api`. Lowercase letters and digits, in hyphen-separated groups, at most 64 characters |
| `--server <url>` | yes | The server's URL, such as `https://cairn.lan:8788`. It must not carry a user name or a password |

Writes `.cairn.toml` in the project directory: `CLAUDE_PROJECT_DIR` when it is set, otherwise the working directory. It changes nothing that is already there. It refuses to run from a working directory outside `CLAUDE_PROJECT_DIR`, when that is set. It does not contact the server. Missing flags are a usage error, exit 2.

The help says the file is written whole or not at all where the filesystem has hard links, and directly where it does not. Issue #200 is open on the wording.

### `export`

```bash
cairn export task [id]
cairn export project
```

- `export task` prints one task as Markdown on stdout: its brief, its steps, its events and its close. Without an id it exports the active task. With no active task it exits 1 and says to give an id.
- `export project` prints every task of the project, open and closed, in the same form, and says so when there are none.

Both read through the server. An argument that is not an id, or a second argument to `export project`, is a usage error, exit 2.

### `hook`

```bash
cairn hook session-start
cairn hook pre-compact
cairn hook stop
```

Output for a client's hook. Claude Code runs these; you run them by hand only to see what they print. They never block a session: a hook exits 0 even when there is nothing to say.

- `session-start` prints the active task, its steps and its directives, as a block of text. With no project it prints `no context added` and exits 0.
- `pre-compact` prints the same block. In Claude Code, the output of this hook is not added to the agent's context. [Connecting-Agents](Connecting-Agents) explains the hooks.
- `stop` prints a warning only when a step of the active task is active with no summary. Otherwise it prints nothing.

`hook --help` is an unknown event, exit 2.

### `enable`

```bash
cairn enable --agent claude-code [--dry-run]
cairn enable --agent opencode [--dry-run]
cairn enable --agent pi [--dry-run]
```

| Flag | Required | Meaning |
|---|---|---|
| `--agent <name>` | yes | `claude-code`, `opencode` or `pi`. The last two are EXPERIMENTAL |
| `--dry-run` | no | Print what would be added, and write nothing |

What each one writes is in [Connecting-Agents](Connecting-Agents#what-enable-writes). In short: `claude-code` adds three hooks to `.claude/settings.local.json` and keeps the rest of the file. It writes no MCP entry. Before it rewrites a file, it copies the original to the user's cache directory, and prints the path. A file that is not plain JSON is refused and left as it is. Any other agent name is a usage error, exit 2.

`enable` warns when `cairn` is not on the PATH of its own shell, because the hooks run `cairn` by name.

### `status`

```bash
cairn status
```

Prints the project, the server and the active task, or `task none active`. It takes no arguments. With no project it exits 1 and says how to set one.

### `task`

```bash
cairn task current [--agent <role>] [--json]
cairn task get <id> [--section brief|steps|events|close] [--limit <n>] [--cursor <c>] [--step <n> | --step-id <id>] [--json]
cairn task list [--status <s>]... [--limit <n>] [--cursor <c>] [--json]
cairn task start --title <t> --goal <g> [--context <c>] [--constraints <c>] [--priority <p>] [--queue] [--json-input] [--agent <a>] [--json]
cairn task update <id> [--status active|blocked|abandoned] [--reason <r>] [--priority <p>] [--owner <o>] [--next-action <n>] [--blockers <b>] [--title <t>] [--goal <g>] [--context <c>] [--constraints <c>] [--json-input] [--agent <a>] [--json]
cairn task close <id> --outcome done|failed [--summary <s>] [--no-lessons-reason <r>] [--json-input] [--agent <a>] [--json]
```

| Subcommand | What it does | Flags |
|---|---|---|
| `current` | The active task, its steps, recent events and directives | `--agent` (default `CAIRN_AGENT`), `--json` |
| `get` | One task in full, or one section of it | `--section` (`brief`, `steps`, `events` or `close`; empty for all), `--limit` (events only: 20 by default, 100 at most), `--cursor` (events only), `--step` (events only, by number), `--step-id` (events only, by id), `--json` |
| `list` | Recent tasks, most recently updated first | `--status` (repeat for more: `open`, `active`, `blocked`, `done`, `failed`, `abandoned`; empty lists all), `--limit` (20 by default, 100 at most), `--cursor`, `--json` |
| `start` | Start a task. It is active unless `--queue` is given | `--title` and `--goal` (required), `--context`, `--constraints`, `--priority` (`low`, `normal`, `high` or `urgent`; `normal` by default), `--queue`, `--json-input`, `--agent`, `--json` |
| `update` | Change a task. Only the flags given change | `--status` (`active`, `blocked` or `abandoned`), `--reason` (required for `blocked` and `abandoned`), `--priority`, `--owner`, `--next-action`, `--blockers`, the brief flags `--title`, `--goal`, `--context` and `--constraints` (only before any step starts), `--json-input`, `--agent`, `--json` |
| `close` | Close a task as `done` or `failed` | `--outcome` (required), `--summary`, `--no-lessons-reason`, `--json-input`, `--agent`, `--json` |

Notes:

- `task start --json-input` reads `criteria`, `key_files` and `steps` from standard input, as one JSON object.
- `task update --json-input` reads `criteria` and `key_files`. A list given replaces the whole list.
- `task close --json-input` reads `criteria`, `lessons`, `mistakes` and `deliverables`. A close needs what the project's close policy asks for: a summary, lessons or a `--no-lessons-reason`, and mistakes, which may be `[]`. A refused close exits 5 and names what is missing.
- `task update --status` cannot set `done` or `failed`. A task closes with `task close`.
- `task get <id>` with no id is a usage error: `name one task id`.

Example:

```bash
cairn task start --title "Add the export flag" --goal "The export command takes a format flag." --agent main --json
```

**You should see** one line of JSON, `{"id":"task-YYYYMMDD-NN","status":"active"}`.

### `step`

```bash
cairn step add <task-id> --title <t> [--after <n> | --after-id <id>] [--assignee <a>] [--agent <a>] [--json]
cairn step update <task-id> <number> --action start|finish|fail|skip [--summary <s>] [--assignee <a>] [--agent <a>] [--json]
cairn step update <task-id> --step-id <id> --action start|finish|fail|skip [--summary <s>] [--assignee <a>] [--agent <a>] [--json]
cairn step plan <task-id> --json-input [--agent <a>] [--json]
```

| Subcommand | What it does | Flags |
|---|---|---|
| `add` | Insert a step | `--title` (required), `--after` (a step number; `0` puts it first), `--after-id`, `--assignee`, `--agent`, `--json`. Left out, it goes last |
| `update` | Start, finish, fail or skip a step | `--action` (required: `start`, `finish`, `fail` or `skip`), `--summary` (needed for `finish`, `fail` and `skip`), `--step-id`, `--assignee`, `--agent`, `--json` |
| `plan` | Replace the task's pending steps | `--json-input` (required), `--agent`, `--json` |

Notes:

- `step update` takes the step by its number, as a positional argument, or by `--step-id`. Give one or the other. The number is positional, so `step update --help` lists the flags only. The usage line is in the error message, and in this table.
- `step plan --json-input` reads `{"steps":[{"title":"...","agent":"..."}]}` from standard input. `agent` is optional.

Example:

```bash
cairn step update task-20261106-01 1 --action finish --summary "The flag is added" --agent hicks
```

### `event`

```bash
cairn event log <task-id> --kind note|blocked|unblocked|handoff|decision|review|custom --text <t> [--step <n> | --step-id <id>] [--data <json>] [--agent <a>] [--json]
```

| Flag | Required | Meaning |
|---|---|---|
| `--kind` | yes | `note`, `blocked`, `unblocked`, `handoff`, `decision`, `review` or `custom`. The server writes the others itself |
| `--text` | yes | What happened. `-` reads standard input |
| `--step`, `--step-id` | no | The step the event is about. Left out, it is about the task |
| `--data` | for `review` | A JSON object. A review needs `verdict` (`approved` or `not-approved`), `critical` and `warnings`. `-` reads standard input |
| `--agent`, `--json` | no | As for the other write verbs |

### `learning`

```bash
cairn learning add --kind finding|pattern|agent-note --title <t> --body <b> [--target <t>] [--rationale <r>] [--example <e>] [--task-id <id>] [--agent <a>] [--json]
cairn learning list [--status <s>]... [--kind <k>]... [--target <t>] [--limit <n>] [--cursor <c>] [--json]
```

| Subcommand | What it does | Flags |
|---|---|---|
| `add` | Propose a learning. It is stored as `proposed` | `--kind` (required), `--title` and `--body` (required), `--target`, `--rationale`, `--example`, `--task-id`, `--agent`, `--json` |
| `list` | The learnings of this project | `--status` (repeat: `proposed`, `accepted`, `rejected`, `applied`, `retired` or `superseded`), `--kind` (repeat: `finding`, `pattern` or `agent-note`), `--target`, `--limit` (20 by default, 100 at most), `--cursor`, `--json` |

`learning list` lists one project. It has no `--all-projects` flag, by decision D31. The MCP tool `learning_list` does take `all_projects`.

Accepting or rejecting a learning is an operator decision, and is made in the web UI. The CLI cannot do it.

### `directive`

```bash
cairn directive list [--agent <role>] [--json]
```

Lists the directives that apply to this project, ranked agent, project, global, at most 12. It does not read `CAIRN_AGENT`. Without `--agent` it lists every role's agent-scoped directives. Directives are ratified in the web UI, not here.

### `recall`

```bash
cairn recall --goal <text> [--agent <role>] [--limit <n>] [--json]
```

| Flag | Required | Meaning |
|---|---|---|
| `--goal` | yes | What the task is for, in a sentence. `-` reads standard input |
| `--agent` | no | The caller's role. It names the agent-scoped directives. Left out, `CAIRN_AGENT` is used |
| `--limit` | no | Items per list: 5 by default, 100 at most |
| `--json` | no | One line of JSON |

Prints the directives that apply, the lessons and mistakes from past tasks, and similar tasks. A goal with no word that can match gives directives only.

### `search`

```bash
cairn search --query <text> [--kind <k>]... [--all-projects] [--limit <n>] [--cursor <c>] [--json]
```

| Flag | Meaning |
|---|---|
| `--query` | Required. The text to find: plain words, which no operator reads as syntax. `-` reads standard input |
| `--kind` | Repeat for more: `task`, `event`, `learning`, `directive`, `note`, `lesson` or `mistake`. Left out searches all of them |
| `--all-projects` | Search every project, not only this one |
| `--limit` | Hits per page: 10 by default, 100 at most |
| `--cursor` | The next value from the last page |
| `--json` | One line of JSON |

### `prime`

```bash
cairn prime [--agent <role>] [--json]
```

The session-start view, for an agent that has only a shell. It is the same content as the session-start hook. With no project it exits 3.

### `doctor`

```bash
cairn doctor [--agent claude-code|opencode|pi]
```

Checks the binary, the project file, the server and the connection, and changes no project data. It prints one line for each check:

| Line | What it checks |
|---|---|
| `binary` | The running binary and its version |
| `project` | The project file, or `CAIRN_PROJECT` |
| `server` | The server URL |
| `key` | The key, from `CAIRN_API_KEY` or `client.env` |
| `reach` | Whether the server answers as a Cairn server and accepts the key. This call sends no project. It sets the key's last-used time, at most once a minute |

With `--agent claude-code`, it also lists the Cairn hooks in the Claude Code settings files, and runs each hook once against the server. The first read of a project the server has not seen creates the project's row, as the hooks do. With `--agent opencode` or `--agent pi`, it reports whether the registration and the instructions that `enable` writes are in place. Both of those are EXPERIMENTAL.

Exit 0 when no line fails, 1 when one does. [Checks](Checks) explains the operator check, `scripts/smoke-check.sh`, which runs `doctor` too.

### `serve`

```bash
cairn serve --db <file> [--addr <host:port>] [--tls-cert <pem> --tls-key <pem>] [--behind-proxy]
```

| Flag | Default | Meaning |
|---|---|---|
| `--db <file>` | none, required | The SQLite file. It is created when it does not exist |
| `--addr <host:port>` | `127.0.0.1:8788` | The address to listen on. Loopback by default |
| `--tls-cert <pem>` | none | The certificate file. Give it with `--tls-key` |
| `--tls-key <pem>` | none | The private key file. Give it with `--tls-cert` |
| `--behind-proxy` | off | A proxy in front terminates TLS, so a non-loopback address needs no certificate files |

The server refuses a non-loopback address unless it has TLS files or `--behind-proxy`, and unless it has at least one active key. A loopback server with no key starts, and refuses every call with 401 until a key exists. The server runs the migrations on start. [Install-Server](Install-Server) gives the steps, and [Upgrade](Upgrade) gives the schema rules.

It has no configuration file: the flags are the settings. It logs one `listening` line at start, with `tls=`, `behind_proxy=` and `active_keys=`. It stops on Ctrl-C or a signal, and exits 0 then.

### `key`

```bash
cairn key add --db <file> --name <name> --scope agent|operator|viewer
cairn key list --db <file>
cairn key revoke --db <file> --name <name>
```

| Subcommand | What it does |
|---|---|
| `add` | Makes a key and prints it once, on stdout. Creates the database if it does not exist, at mode 0600 |
| `list` | Lists the keys: name, scope, created, last used and revoked, in UTC. It needs the database to exist |
| `revoke` | Ends a key at its next call. A revoked name is never reused |

Notes:

- The key is shown once. The database keeps only its SHA-256 hash. If stdout cannot take the key, the key is revoked, and the command exits 1.
- `key list` and `key revoke`, like `key add`, open the database with the migrator. On a database from an older build, they upgrade it. See [Upgrade](Upgrade).
- The key's scope decides the role: `agent` for the MCP tools and the verbs, `operator` for decisions and ratification, and `viewer` for read-only access. [Operations](Operations) has the table.

### `backup`

```bash
cairn backup --db <file> <out>
```

Writes a consistent copy of the server's database to `<out>`, a new file at mode 0600. It works while the server runs. It refuses a file that already exists, and never writes over one. It needs a filesystem with hard links, and says so when it does not have one. It opens the database without migrating it, so the copy keeps its schema. A killed backup can leave a hidden `.cairn-backup-*` folder beside `<out>`, which you remove by hand.

[Operations](Operations) covers the schedule and the checks.

### `restore`

```bash
cairn restore --db <file> <backup>
```

Replaces the database at `<file>` with the backup, after you confirm.

- **Interactive only.** Standard input must be a terminal. A pipe or a script is refused, and nothing changes.
- **Confirmation.** You type the database path. There is no flag to skip it.
- **Checks first.** The backup is opened and checked for its schema version and integrity. A backup from a newer build is refused.
- **Nothing is deleted.** The database's `-wal` is folded in, the database is moved to `<file>.pre-restore-<UTC time>`, and a `-shm` is moved beside it. Every move is printed.
- **Stop the server first.** The restore checks for a running server when it can read the database. It cannot always do so, so stop `cairn serve` yourself.
- **Stray files refuse.** A `-wal` or `-shm` beside a path with no database refuses the restore, and nothing changes.

[Upgrade](Upgrade) has the rollback that uses it. [DEPLOY.md, Restore](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#restore) has the full steps.
