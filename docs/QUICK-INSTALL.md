# Quick install: a central Cairn server for testing (macOS)

_Status as of 2026-11-11, main 18fc813. Each command was checked against the source or `cairn --help`. Steps 1 to 3, 5 and 6 were not run when this was written: they build, write keys, start a server or change Claude Code._

This is the fast path, for copying and pasting. The wiki is the full reference and owns the procedure: [Install-Local](../wiki/Install-Local.md), [Install-Server](../wiki/Install-Server.md) (Linux systemd, TLS and proxies), [Quick-Start](../wiki/Quick-Start.md), [Connecting-Agents](../wiki/Connecting-Agents.md) and [Checks](../wiki/Checks.md). To update later, see [QUICK-UPDATE.md](QUICK-UPDATE.md).

The repo ships no launchd unit and no release binaries. You build `cairn` from source and run `cairn serve` by hand, in a terminal. Go 1.26 is required (`go.mod` declares `go 1.26.0` and `toolchain go1.26.10`). Use two terminals: one runs the server, the other runs everything else.

## 1. Build and install

```bash
cd <repo>                                  # the cairn-memory checkout; clone it first if you have none
git pull --ff-only
make build && bin/cairn version            # expect: cairn <short sha>
mkdir -p ~/.local/bin && install -m 0755 bin/cairn ~/.local/bin/cairn
command -v cairn                           # prints a path; ~/.local/bin must be on PATH
```

## 2. Keys and the client file

```bash
mkdir -p ~/cairn-data
cairn key add --db ~/cairn-data/cairn.db --name laptop --scope agent   # the key prints once: copy it
cairn key add --db ~/cairn-data/cairn.db --name ops --scope operator   # optional: signs in to the web UI
mkdir -p -m 700 ~/.config/cairn
printf 'API key: ' >&2; read -rs K; echo >&2                          # paste the laptop key
(umask 077 && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
ls -l ~/.config/cairn/client.env           # expect -rw-------
```

The key stays in `client.env`. Do not put it in `.cairn.toml` or in an MCP entry.

## 3. Start the server

In the second terminal. Leave it running; Ctrl-C stops it.

```bash
cairn serve --db ~/cairn-data/cairn.db     # 127.0.0.1:8788 by default
```

Back in the first terminal:

```bash
curl -sS http://127.0.0.1:8788/healthz     # {"status":"ok"}
```

Web UI: open `http://127.0.0.1:8788/ui/signin` and sign in with the operator key. An agent key is refused there.

## 4. Point a project at it

```bash
cd <project>
cairn init --project <name> --server http://127.0.0.1:8788
cairn doctor                               # five lines, each ok
```

## 5. Connect Claude Code

From the project folder:

```bash
cairn enable --agent claude-code --dry-run && cairn enable --agent claude-code   # writes .claude/settings.local.json
claude mcp add cairn -- "$(command -v cairn)" mcp   # default scope; syntax from claude mcp add --help, not run end to end
cairn doctor --agent claude-code
```

Restart Claude Code in the project folder.

## 6. Operator check

From the project folder, with the server running:

```bash
CAIRN_API_KEY="$(sed -n 's/^CAIRN_API_KEY=//p' ~/.config/cairn/client.env)" <repo>/scripts/smoke-check.sh --server http://127.0.0.1:8788
```

Expect seven `PASS` lines and `smoke-check: 7 passed, 0 failed, 0 skipped`. The doctor line needs a project folder. [Checks](../wiki/Checks.md) says what each line proves.
