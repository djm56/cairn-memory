# Troubleshooting

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da. Messages are quoted as the code prints them._

Each table gives what you see, why it happens, and what to do. Find the message you have. The exit codes are in [CLI-Reference](CLI-Reference#exit-codes), and the checks that find many of these problems are in [Checks](Checks).

If a symptom is not here, run `cairn doctor` in the project folder. Its five lines show where the fault is.

## Installing and the client

| What you see | Cause | Fix |
|---|---|---|
| `cairn: command not found` | The binary is not on the PATH | Put the directory on your PATH in your shell profile, and open a new terminal. [Install-Local](Install-Local), step 2 |
| `cairn version` prints `cairn dev` | The binary was built with a plain `go build`, without the version stamp | Build with `make build`. [Binaries](Binaries) |
| `no project: run cairn init in the project, or set CAIRN_PROJECT` | No `.cairn.toml` is found, searching up from `CLAUDE_PROJECT_DIR` or the working directory, and `CAIRN_PROJECT` is not set | Go to the project folder, or run `cairn init --project <name> --server <url>` there |
| `no server: run cairn init in the project to write one, or set CAIRN_SERVER` | The `.cairn.toml` has no `server` line, and `CAIRN_SERVER` is not set | Add `server = "..."` to `.cairn.toml`, or set `CAIRN_SERVER` |
| `missing: set CAIRN_API_KEY, or write CAIRN_API_KEY=<key> to /home/you/.config/cairn/client.env` (the binary prints your home folder's full path) | No key on the client | Store the key as [Install-Local](Install-Local), step 4, describes |
| `cairn mcp: invalid: no API key: ...` (exit 1) | `cairn mcp` will not start without a key | As above. The key must be in the environment of the process that starts `cairn mcp`, or in `client.env` |
| The client refuses `client.env`, and says `chmod 600` | The file is readable or writable by another user | `chmod 600 ~/.config/cairn/client.env`, and make the folder `chmod 700` |
| `the server refused the API key` (exit 1, kind `internal`) | The key is wrong, has been revoked, or is malformed | Check the key in `client.env` or `CAIRN_API_KEY`. If it was revoked, ask the operator for a new one. Keys are shown once, so a lost key is replaced |
| A write fails with `read_only` (exit 1, kind `internal`) | The key is a viewer key. Viewers can read and change nothing | Use an agent key for the client. [Operations](Operations) |
| `no_route` (exit 7) | The URL does not reach the Cairn server's API. Often a wrong path, or a site that is not a Cairn server | Check the URL is the server's base address, with no path. `curl <url>/healthz` should answer `{"status":"ok"}` |
| Calls fail, and the URL redirects, for example from `http://` to `https://` | The client does not follow redirects, so the key is never sent to a second address | Use the final address in `.cairn.toml` or `CAIRN_SERVER` |
| `cairn doctor` warns about plain `http` | The server URL is `http://` and the host is not loopback, so the key travels in the clear | Use `https://`, or a tunnel to a loopback address. [DEPLOY.md, A server with no public DNS name](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#a-server-with-no-public-dns-name) |
| `cairn enable` says `needs --agent <name>` (exit 2) | `enable` takes an agent name | `cairn enable --agent claude-code`, or `opencode` or `pi` (both EXPERIMENTAL) |
| `enable` refuses a settings file, saying it is not plain JSON | The file has comments or a syntax error. `enable` leaves it alone | Fix the file by hand, then run `enable` again |
| `enable --agent opencode` refuses, because `opencode.jsonc` exists | A file of that name is in the project, and `enable` will not write `opencode.json` beside it | Remove or rename `opencode.jsonc` yourself, if it is not yours to keep |
| `enable` warns that `cairn` is not on the PATH | The hooks run `cairn` by name | Put the binary on the PATH Claude Code uses. [Binaries](Binaries) |

## Calls and the MCP tools

| What you see | Cause | Fix |
|---|---|---|
| `<verb>: no agent name: give --agent <name>, or set CAIRN_AGENT to one` (CLI, exit 2, on stderr) | A CLI write was made with no agent name, and `CAIRN_AGENT` is not set | Give `--agent <name>`, or set `CAIRN_AGENT` to the caller's name. [CLI-Reference](CLI-Reference#environment-variables) |
| `this call writes, so the caller needs an agent name` (exit 3, over MCP the same message ending `pass your name as the agent argument`) | An MCP write, or a server call, arrived with no agent name | Pass `agent` in the MCP arguments, such as `main` or `hicks` |
| `no task is active; give the task id` (`export task`, exit 1) | `export task` with no id, and no active task | Give the task's id: `cairn export task <id>` |
| `task ... not found` (exit 4) | The id is wrong, or the task is in another project | Check the id with `cairn task list`. A task belongs to one project |
| `step number 9 not found in task ...` (exit 4) | The task has no step with that number | Check the steps with `cairn task get <id> --section steps` |
| `close policy not met: summary, lessons, mistakes` (exit 5) | The project's close policy asks for those fields | Give them. For lessons, use `--json-input` with `{"lessons":[]}` and `--no-lessons-reason`. For mistakes, give `[]` if there are none |
| `task ... is done, and a closed task takes no changes` (exit 5) | The task is already closed | Start a new task. A closed task is final |
| A task start is refused because another task is active | A task is already active, and the project does not allow parallel tasks | Close or block the other task first. To create a task without starting it, use `--queue`, and start it later |
| `the result is N bytes, more than the 65536 a tool result may hold` | A tool result is over the 64 KiB cap | Narrow the call: use `section`, `limit` and `cursor` on `task_get`, `task_list`, `learning_list` or `search`. A brief over the cap cannot be narrowed (issue #191), so shorten the brief |
| `recall` lists no agent-scoped directives over MCP | The adapter sends no caller name on a read | Pass `agent` to `recall` and `task_current`. [MCP-Tools](MCP-Tools#notes-on-the-code) |
| Every MCP call is refused with a reason that says to restart | The project was not found when the session started, and the project is read once per session | Fix the project (`.cairn.toml` or `CAIRN_PROJECT`), then restart the MCP session |
| `cairn mcp` refuses `--help` (exit 2) | `cairn mcp` takes no arguments | Run `cairn mcp` with no arguments, from the MCP client |
| `no directives`, `no tasks` or `no learnings` | The list is empty | Not an error, and the exit is 0 |
| `no context added` from `cairn hook session-start` | No project is found, so the hook has nothing to add. The hook exits 0 by design | Set up the project as above |

## Claude Code and the hooks

| What you see | Cause | Fix |
|---|---|---|
| After `/compact`, the agent does not know the task | The `SessionStart` hook is missing, or was installed without the `compact` matcher | Run `cairn doctor --agent claude-code`. Then `cairn enable --agent claude-code`, and restart Claude Code |
| `cairn doctor --agent claude-code` shows a hook as not installed | `enable` has not been run in this project, or the settings file was changed | Run `cairn enable --agent claude-code` |
| The `PreCompact` hook's output does not reach the agent | Claude Code does not add that hook's output to the context. This is how it works, not a fault | None needed. The `SessionStart` hook carries the task. [Connecting-Agents](Connecting-Agents) |
| `cairn` is not found when Claude Code runs a hook | The directory with `cairn` is not on the PATH that Claude Code uses | Put the binary in a directory on that PATH. [Binaries](Binaries) |

## The server

| What you see | Cause | Fix |
|---|---|---|
| `the address is not loopback, so it needs TLS: give --tls-cert and --tls-key, or --behind-proxy if a proxy in front terminates TLS` | `--addr` is not a loopback address, and there is no TLS and no proxy flag | Add the certificate files, or `--behind-proxy` when a proxy terminates TLS. [Install-Server](Install-Server) |
| `the address is not loopback, so it needs at least one active API key (DESIGN §8)` | A network address and no key | Make a key with `cairn key add`, then start the server |
| `--tls-cert and --tls-key go together: give both, or neither` (exit 2) | One of the two files is missing | Give both, or neither |
| `no active API key: every call will be refused with 401 until a key exists` | A loopback server with no key. It starts, and refuses calls | Make a key. This line is expected until you do |
| `database is at schema version 8, newer than this build (7)` | The database was made or changed by a newer build. `serve` and the `key` commands print this | Use the newer build, or roll back with the pre-upgrade backup. An older build cannot read a newer database. [Upgrade](Upgrade) |
| `store: <path> is at schema version 8, newer than this build (7)` | The same, from `cairn backup` or `cairn restore` | Run the backup with the newer build. The older one cannot copy a newer database. [Upgrade](Upgrade) |
| The server does not start after an upgrade, and the journal shows a migration error | A migration failed. The earlier migrations stay applied | Roll back: the old binary and the pre-upgrade backup. [Upgrade](Upgrade), step 9 |
| The service restarts in a loop after you revoke a key | The server listens beyond loopback, and revoking the last key leaves it with none | Make a new key, then restart. Revoke a key only after a replacement is in place |
| The server answers `a call is a POST with a JSON body` (405) | A `GET` was sent to an API route | Send a `POST` with a JSON body. Only `/healthz` takes `GET` |
| `unsupported_media_type` (exit 8) | The request body is not `application/json` | Send `Content-Type: application/json`. The CLI and the MCP adapter do this for you |
| `curl` to `/healthz` gives a certificate error | The certificate does not match the name, or the client does not trust its authority | Fix the certificate, or the client's trust. Do not use `curl -k` to get past it |
| The server renewed its certificate, and clients still see the old one | The server reads the certificate once, at start | `sudo systemctl restart cairn`. [Operations](Operations) |
| The log shows `tls=false` behind a proxy | Expected: the proxy terminates TLS | None needed |
| `make push` says `Set SERVER, as in: make push SERVER=user@host` | `SERVER` was not given | Give it: `make push SERVER=user@host` |
| On the server, `./cairn-linux version` fails, typically with `exec format error` | The binary is built for the other CPU | On an `arm64` server, run `make push` with `LINUX_ARCH=arm64`. Check the CPU with `uname -m` |

## The web UI

| What you see | Cause | Fix |
|---|---|---|
| Every form, sign-in included, says `The form could not be verified.` | Either the proxy rewrote the `Host` header, or `--behind-proxy` is missing while the browser uses `https`. The server's log line names the `Host` and `Origin` it compared | Keep the `Host` header in the proxy, and give `--behind-proxy` for setup (b). [Install-Server](Install-Server), Part 3b |
| Sign-in does not stick, over plain `http` | The session cookie is `__Secure-cairn_ui` when served behind a proxy, and a browser refuses it on a page that is not `https` | Open the UI at the proxy's `https` address |
| Sign-in refuses the key, with a message about an agent key | An agent key cannot sign in to the web UI | Use an operator key or a viewer key |
| A viewer can open a page but not change it | Expected. A viewer key is read-only | Use an operator key for changes |

## Backup and restore

| What you see | Cause | Fix |
|---|---|---|
| `cairn restore: stdin is not a terminal ... nothing was changed` | `restore` was run from a pipe or a script | Run it from an interactive shell, as the `cairn` user |
| `restore` refuses, naming a `-wal` or `-shm` beside the path | A stray file beside a path with no database. Restoring over it would let SQLite replay it | Move those files out of the folder by hand, then run the restore again |
| The server refuses to start, and names a `.pre-restore-` file | An interrupted restore. The server will not create an empty database over it | Put the files back as the restore's message says, then start the server. [Operations](Operations), Restore |
| `backup` refuses the output name | The file already exists. `backup` never overwrites | Choose a new name |
| `backup` says the filesystem has no hard links | The output's folder is on a filesystem that does not support hard links | Back up to a folder on a filesystem that supports them |
| A hidden `.cairn-backup-*` folder is left beside the output | A backup was killed partway | Check it, then remove it by hand. `backup` does not remove it |

## Install checks and CI

| What you see | Cause | Fix |
|---|---|---|
| `scripts/smoke-check.sh` shows `doctor` as `FAIL`, and the `project` line fails | The script was run outside a project folder | Run it from inside the project, or set `CAIRN_PROJECT`. [Checks](Checks) |
| `smoke-check` shows `key` as `FAIL` with `401` or `403` | The key is wrong or revoked | As above, for the key |
| `smoke-check` shows `mcp` as `FAIL` on a slow machine | The check waits one second for replies | Run it again. If it fails again, run `cairn mcp` by hand and send it `initialize` |
| `check-docs.sh` says `changelog: cannot diff against ...` | The base commit is not in the checkout | Fetch the history, with full depth |
| The wiki publish says it cannot clone the wiki, and asks whether the wiki was created | The GitHub wiki has no first page | Create the first page on GitHub, then publish again |
