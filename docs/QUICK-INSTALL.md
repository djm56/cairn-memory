# Quick install: a central Cairn server for testing (macOS)

_Status as of 2026-11-11, checked against main at time of writing. Not run end to end when this was written._

This is the fast path, for copying and pasting. Use it only on a Mac with no Cairn install. If `~/.config/cairn/client.env` or `~/cairn-data/cairn.db` already exists, use [QUICK-UPDATE.md](QUICK-UPDATE.md) instead.

Replace these placeholders before you paste: `<repo>` is your cairn-memory checkout, `<project>` is the project folder, and `<name>` is the project name (lowercase letters, digits and single hyphens, at most 64 characters).

The wiki is the full reference and owns the procedure: [Install-Local](../wiki/Install-Local.md), [Install-Server](../wiki/Install-Server.md) (Linux systemd, TLS and proxies), [Quick-Start](../wiki/Quick-Start.md), [Connecting-Agents](../wiki/Connecting-Agents.md) and [Checks](../wiki/Checks.md). To update later, see [QUICK-UPDATE.md](QUICK-UPDATE.md).

The repo ships no launchd unit and no release binaries. You build `cairn` from source and run `cairn serve` by hand, in a terminal. Go 1.26 is required (`go.mod` declares `go 1.26.0` and `toolchain go1.26.10`). Use two terminals: one runs the server, the other runs everything else.

## 1. Build and install

Clone the repo first if you have no checkout. Then, in `<repo>`:

```bash
cd <repo>
git switch main && git pull --ff-only
make build && bin/cairn version
```

Expect `cairn` followed by a short commit hash. If the hash ends in `-dirty`, stop: the checkout has local changes, so deal with them before you go on. Build with `make build`, not `go build`, which prints `cairn dev`.

```bash
mkdir -p ~/.local/bin && install -m 0755 bin/cairn ~/.local/bin/cairn
command -v cairn
```

`command -v cairn` prints a path. If it prints nothing, add `~/.local/bin` to your PATH.

## 2. Keys and the client file

Run the two key lines first. The key prints once: copy the laptop key. The operator line is optional and signs in to the web UI. If either `key add` fails, stop. A name is never reused, so a second run with `laptop` fails too.

```bash
mkdir -p ~/cairn-data
cairn key add --db ~/cairn-data/cairn.db --name laptop --scope agent
cairn key add --db ~/cairn-data/cairn.db --name ops --scope operator
```

Then store the key. At the prompt, paste the laptop key.

```bash
mkdir -p -m 700 ~/.config/cairn && chmod 700 ~/.config/cairn
printf 'API key: ' >&2; read -rs K; echo >&2
[ -n "$K" ] && (umask 077 && set -C && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
ls -l ~/.config/cairn/client.env
```

The write refuses to overwrite an existing file and refuses an empty paste. If it prints `cannot overwrite existing file` (bash) or `file exists` (zsh), stop. A `client.env` already holds a working key, and pasting over it would replace that key. If the paste was empty, nothing is written: run the block again and paste. The `ls` line should show `-rw-------`.

The key stays in `client.env`. Do not put it in `.cairn.toml` or in an MCP entry.

## 3. Start the server

In the second terminal, run the server and leave it running. Ctrl-C in that terminal stops it.

```bash
cairn serve --db ~/cairn-data/cairn.db
```

It listens on 127.0.0.1:8788 by default. Back in the first terminal, check it. Expect `{"status":"ok"}`.

```bash
curl -sS http://127.0.0.1:8788/healthz
```

Web UI: open `http://127.0.0.1:8788/ui/signin` and sign in with the operator key. An agent key is refused there.

## 4. Point a project at it

```bash
cd <project>
cairn init --project <name> --server http://127.0.0.1:8788
cairn doctor
```

`cairn doctor` prints five lines. Each should say ok.

## 5. Connect Claude Code

From the project folder. The first line only prints what it would change; the second writes `.claude/settings.local.json`.

```bash
cairn enable --agent claude-code --dry-run && cairn enable --agent claude-code
claude mcp add cairn -- "$(command -v cairn)" mcp
cairn doctor --agent claude-code
```

`claude mcp add` without `--scope` adds the entry for this project only (local scope). For every project, use user scope instead: see [Connecting-Agents](../wiki/Connecting-Agents.md). The syntax comes from `claude mcp add --help`; this line was not run end to end.

Restart Claude Code in the project folder.

## 6. Operator check

From the project folder, with the server running:

```bash
CAIRN_API_KEY="$(sed -n 's/^CAIRN_API_KEY=//p' ~/.config/cairn/client.env)" <repo>/scripts/smoke-check.sh --server http://127.0.0.1:8788
```

Expect seven `PASS` lines and `smoke-check: 7 passed, 0 failed, 0 skipped`. The doctor line needs a project folder. [Checks](../wiki/Checks.md) says what each line proves.
