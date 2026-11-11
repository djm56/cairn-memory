# Quick-Start

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da. Live progress is on the [milestones](https://github.com/djm56/cairn-memory/milestones)._

This is the shortest correct path on one machine. It builds `cairn`, runs a server on loopback, makes a key, points a project at the server, connects Claude Code and runs the operator check. Each step ends with **You should see**. If a line differs, stop and check [Troubleshooting](Troubleshooting) before you go on.

For each step in more detail, see [Install-Local](Install-Local). For a server that other machines reach, see [Install-Server](Install-Server). For a copy-paste runbook to install or update a test server, see [QUICK-INSTALL](https://github.com/djm56/cairn-memory/blob/main/docs/QUICK-INSTALL.md) and [QUICK-UPDATE](https://github.com/djm56/cairn-memory/blob/main/docs/QUICK-UPDATE.md).

You need Go 1.26 or later (the version `go.mod` declares), git, and a shell. There is no C toolchain to install. The commands below are for bash or zsh on macOS or Linux.

## 1. Build the binary

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
make build
bin/cairn version
```

**You should see** `cairn` followed by a short commit hash, such as `cairn 8b651da`. A checkout with uncommitted changes adds `-dirty` to the hash. Use `make build`: a plain `go build` prints `cairn dev`.

## 2. Put it on your PATH

```bash
mkdir -p ~/.local/bin
install -m 0755 bin/cairn ~/.local/bin/cairn
command -v cairn
```

If `command -v` prints nothing, `~/.local/bin` is not on your PATH. Add it in your shell profile and open a new terminal.

**You should see** the full path to the binary, such as `/Users/you/.local/bin/cairn`. Then `cairn version` prints the same hash as step 1.

## 3. Start a server on this machine

Open a second terminal for the server and leave it running there.

```bash
mkdir -p ~/cairn-data
cairn serve --db ~/cairn-data/cairn.db
```

**You should see** a log line with `msg=listening addr=127.0.0.1:8788`, `tls=false`, `behind_proxy=false` and `active_keys=0`, and the line `cairn serve: no active API key: every call will be refused with 401 until a key exists`. Both are correct until step 4. The server listens on loopback only. Stop it later with Ctrl-C.

## 4. Make a key for the agent

In the first terminal:

```bash
cairn key add --db ~/cairn-data/cairn.db --name laptop --scope agent
```

**You should see** one line starting `cairn_` and 43 characters after it, on stdout. The server prints it once. On stderr you should see `cairn key add: the key is shown once above: store it now, it cannot be shown again`. Copy it now, or make a new key: the database keeps only a hash.

Put the key in a file that only you can read. Then type the key when the prompt asks for it. The key is not echoed and does not go into your shell history:

```bash
mkdir -p -m 700 ~/.config/cairn && chmod 700 ~/.config/cairn
printf 'API key: ' >&2; read -rs K; echo >&2
(umask 077 && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
ls -l ~/.config/cairn/client.env
```

**You should see** the `API key:` prompt, then the `ls` line starting `-rw-------`.

## 5. Point a project at the server

Go to the folder of the project you want Cairn to record. Use a project folder, not the `cairn-memory` clone.

```bash
cd ~/path/to/your/project
cairn init --project my-project --server http://127.0.0.1:8788
```

Project names are lowercase letters and digits, in hyphen-separated groups, at most 64 characters. `cairn init` writes `.cairn.toml` and does not contact the server.

**You should see** `cairn init: wrote .../.cairn.toml with project "my-project" and server "http://127.0.0.1:8788".` and a line saying the file holds no key. The file is safe to commit.

## 6. Check the install

```bash
cairn doctor
echo $?
```

**You should see** five lines, `binary`, `project`, `server`, `key` and `reach`, each marked `ok`, and then `0`. `doctor` changes nothing in the project. If a line says `fail`, the line says what to set.

## 7. Connect Claude Code

```bash
cairn enable --agent claude-code
claude mcp add cairn -- "$(command -v cairn)" mcp
claude mcp list
```

**You should see** three lines from `enable`: `SessionStart added`, `PreCompact added` and `Stop added`, then `wrote` and the path of `.claude/settings.local.json`. Run `enable` again and the lines say `already present`, and nothing changes. Then `claude mcp list` shows `cairn`. `enable` writes the hooks into `.claude/settings.local.json` in the project. It does not register the MCP server: the `claude mcp add` line does that. The `claude mcp add` syntax is from Claude Code 2.1.311; this page was not run end to end in Claude Code. [Connecting-Agents](Connecting-Agents) has the details and the other clients.

The line has no `--scope`, so Claude Code's default, `local`, applies. `--scope user` would make the entry available in every project. In a project with no `.cairn.toml` that names a `server`, and no `CAIRN_SERVER`, `cairn mcp` exits 1 at start, so that entry would fail there.

Start Claude Code in the project folder. `cairn mcp` finds the server and the key the same way every other command does: `.cairn.toml` from `CLAUDE_PROJECT_DIR` when Claude Code sets it, and from the working directory otherwise. How Claude Code sets those for an MCP server is the M5 check, and this page does not claim it. [Connecting-Agents](Connecting-Agents) has the details.

## 8. Run the operator check

Run the check from the project folder, with the key read from the file in step 4. Use the path to your clone of the repository:

```bash
cd ~/path/to/your/project
CAIRN_API_KEY="$(sed -n 's/^CAIRN_API_KEY=//p' ~/.config/cairn/client.env)" \
  ~/path/to/cairn-memory/scripts/smoke-check.sh --server http://127.0.0.1:8788
echo $?
```

**You should see** seven lines, each starting `PASS`: `binary`, `version`, `doctor`, `healthz`, `refused`, `key` and `mcp`. The last line reads `smoke-check: 7 passed, 0 failed, 0 skipped`, and then `0`. [Checks](Checks) says what each line proves.

## Next

- Start a task with `cairn task start`, or let Claude Code do it. [CLI-Reference](CLI-Reference) lists the verbs, and [MCP-Tools](MCP-Tools) lists what the agent can call.
- Keep the server running while you work. To stop it, press Ctrl-C in its terminal.
- For a server that other machines reach, read [Install-Server](Install-Server). Upgrades are in [Upgrade](Upgrade).
- If a step did not match, read [Troubleshooting](Troubleshooting).
