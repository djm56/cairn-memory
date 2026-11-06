# Install locally

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da._

This page is the detailed version of [Quick-Start](Quick-Start), for a developer machine. It covers what you need, what each step does, the files `cairn` reads and writes, and the environment variables it reads. Use it when you want to know why a step works, or when the Quick-Start does not match what you see.

Every command runs in a shell. Commands are for bash or zsh on macOS or Linux. Windows is not a supported target.

## What you need

- **Go 1.26 or later.** [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod) declares `go 1.26.0` and `toolchain go1.26.10`. The Go version is the one in `go.mod`, not the newest Go you have.
- **git.** `make build` reads the version from `git describe`. Without git, the build still works but prints `cairn dev`.
- **No C toolchain.** The SQLite driver is pure Go (`modernc.org/sqlite`), so `CGO_ENABLED=0` works.
- **A server to talk to.** Either one you run on this machine, which the first half of this page sets up, or a shared server whose operator gives you a key.

A `cairn` binary never opens a database. Every `cairn` command, and `cairn mcp`, sends its calls over HTTP to `cairn serve`. The machine that runs the client needs a server URL and a key, and nothing else.

## 1. Get the source and build

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
make build
bin/cairn version
```

`make build` runs `go build` with the version stamped in, and writes `bin/cairn`. The `bin/` folder is git-ignored. [Binaries](Binaries) lists every Makefile target and the version strings you can see.

**You should see** `cairn <hash>`, such as `cairn 8b651da`.

## 2. Install the binary on your PATH

The Makefile has no install target. Copy the file yourself. `~/.local/bin` is a common choice on macOS and Linux:

```bash
mkdir -p ~/.local/bin
install -m 0755 bin/cairn ~/.local/bin/cairn
command -v cairn
cairn version
```

Claude Code runs `cairn` by name in its hooks (see step 9), so the directory must be on the PATH that Claude Code uses. `cairn enable` warns when `cairn` is not on the PATH of the shell it runs in.

**You should see** the full path to the file, then `cairn <hash>`.

## 3. Choose a server

- **A server on this machine.** Start one with `cairn serve`. It listens on `127.0.0.1:8788` by default. Use a path outside your projects for the database:

  ```bash
  mkdir -p ~/cairn-data
  cairn serve --db ~/cairn-data/cairn.db
  ```

  Loopback needs no TLS. The server starts with no key, but refuses every call with 401 until a key exists. Make one as in step 4.

- **A shared server.** Ask its operator for the URL and an agent key. The URL is `https://` when the server uses TLS. [Install-Server](Install-Server) is the page for running one.

## 4. Make a key, and store it on the client

Keys are made on the server, not on the client. On the machine that runs `cairn serve`:

```bash
cairn key add --db ~/cairn-data/cairn.db --name laptop --scope agent
```

`--scope` is `agent` for the MCP tools and the CLI verbs that write. `operator` is for decisions and ratification, and `viewer` is read-only. [Operations](Operations) explains the three. `key add` prints the key once, on stdout, and creates the database when it does not exist.

Store the key on the client in one of two places. Cairn reads them in this order:

1. The `CAIRN_API_KEY` environment variable.
2. `~/.config/cairn/client.env`, a file with one line, `CAIRN_API_KEY=cairn_...`.

The file is the usual choice. Make the folder and the file private, and type the key at the prompt so it stays out of your shell history:

```bash
mkdir -p -m 700 ~/.config/cairn && chmod 700 ~/.config/cairn
printf 'API key: ' >&2; read -rs K; echo >&2
(umask 077 && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
ls -l ~/.config/cairn/client.env
```

**You should see** `-rw-------` on the `ls` line. `cairn` refuses a `client.env` that another user can read or write, and says `chmod 600`. The client does not follow redirects, so the key is never sent to a second address. Do not put the key in `.cairn.toml` or in any file you commit.

The path is always `~/.config/cairn/client.env`. `XDG_CONFIG_HOME` is not read.

## 5. Write the project file

Run this in the project folder, not in the `cairn-memory` clone:

```bash
cd ~/path/to/your/project
cairn init --project my-project --server http://127.0.0.1:8788
```

`cairn init` writes `.cairn.toml` in the project directory, which is `CLAUDE_PROJECT_DIR` when that is set, and the working directory otherwise. It changes nothing that is already there. It does not contact the server. It refuses a server URL that carries a user name or a password, because the file is committed.

A `.cairn.toml` looks like this:

```toml
# Cairn project file (docs/DESIGN.md §2.1). Commit it. It holds no key.
project = "my-project"
server = "http://127.0.0.1:8788"
```

Two rules follow from the code:

- **There is no default project.** A command run where no `.cairn.toml` is found, and with no `CAIRN_PROJECT`, stops with `no project: run cairn init in the project, or set CAIRN_PROJECT`.
- **The project name** is lowercase letters and digits in hyphen-separated groups, at most 64 characters.

The project is found from `CAIRN_PROJECT` first, then from the nearest `.cairn.toml`, searched upward from `CLAUDE_PROJECT_DIR` when it is set and from the working directory otherwise. The server is found from `CAIRN_SERVER` first, then from the `server` line.

## 6. Check it

```bash
cairn doctor
cairn status
```

`doctor` checks five things, and changes nothing in the project:

| Line | What it checks | When it says `fail` |
|---|---|---|
| `binary` | The running binary and its version | Never, if it runs |
| `project` | The project file, or `CAIRN_PROJECT` | No `.cairn.toml` above the folder and no `CAIRN_PROJECT` |
| `server` | The server URL, from `CAIRN_SERVER` or `.cairn.toml` | Neither is set |
| `key` | The key, from `CAIRN_API_KEY` or `client.env` | No key is found |
| `reach` | Whether the server answers as a Cairn server and accepts the key | The server does not answer, or refuses the key |

`doctor` exits 0 when no line is `fail`, and 1 when one is. A `skip` line, such as `reach` when there is no key, is not a failure. Its `reach` line makes one authenticated call. That call sends no project, so it reads and writes no project data. It does set the key's last-used time, at most once a minute. [Checks](Checks) covers the operator check, `smoke-check.sh`, which runs `doctor` and more.

`cairn status` prints the project, the server and the active task, or `task none active`.

## 7. Environment variables

These are the environment variables `cairn` reads. Set them in the shell, or in the environment of the process that runs `cairn`, such as Claude Code's MCP entry.

| Variable | Meaning | Overrides |
|---|---|---|
| `CAIRN_API_KEY` | The API key, `cairn_...` | `client.env` |
| `CAIRN_SERVER` | The server's base URL | the `server` line of `.cairn.toml` |
| `CAIRN_PROJECT` | The project name | the `project` line of `.cairn.toml` |
| `CAIRN_AGENT` | The calling agent's name or role | the `--agent` flag's default |
| `CLAUDE_PROJECT_DIR` | Where to look for `.cairn.toml`, and where `init` writes it | the working directory |

The full list of verbs and flags that use them is in [CLI-Reference](CLI-Reference).

## 8. Files on this machine

| File | Written by | Read by | Holds a key |
|---|---|---|---|
| `.cairn.toml` in the project | `cairn init`, or by hand | every project-scoped command | no |
| `~/.config/cairn/client.env` | you | every command that calls the server | yes |
| `.claude/settings.local.json` in the project | `cairn enable --agent claude-code` | Claude Code | no |
| `<user cache>/cairn/backups/<project>/` | `cairn enable`, before it rewrites a file | you | no |
| `bin/cairn` in the clone | `make build` | you | no |
| `~/.local/bin/cairn` (or your choice) | you, with `install` | the shell, and Claude Code's hooks | no |

`enable` copies the original of any file it rewrites into the user cache directory, which is `~/Library/Caches` on macOS. On Linux it is `$XDG_CACHE_HOME/cairn/backups/<project>` when `XDG_CACHE_HOME` is set, and `~/.cache/cairn/backups/<project>` when it is not. Nothing prunes that folder. Keep a copy of a backup you need.

## 9. Connect the client

`cairn enable --agent claude-code` installs three hooks into `.claude/settings.local.json`. It keeps every other entry in the file. `--dry-run` prints what it would add and writes nothing:

```bash
cairn enable --agent claude-code --dry-run
cairn enable --agent claude-code
```

It does not register the MCP server. That is a separate step, shown in [Connecting-Agents](Connecting-Agents). The Claude Code plugin, which will do both, is M5 work.

## Run a local server for development

The same server serves a whole team. For one machine, the loopback default needs no certificate and no proxy. Start it with `cairn serve --db <path>`. Add `--addr` only when you need another loopback address, and keep the database outside any project folder. The repository's `.gitignore` covers `*.db`, but another repository may not.

## If a step does not match

[Troubleshooting](Troubleshooting) lists the messages Cairn prints, with their causes and fixes.
