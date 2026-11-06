# Connecting agents

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. #206 is open. Checked against the code at 8b651da. The Claude Code commands were checked against the `claude` CLI, version 2.1.311, and not run end to end._

An agent reaches Cairn in one of two ways. Clients that speak MCP connect to `cairn mcp`, which talks to the server. Agents with only a shell use the `cairn` command-line verbs. Each client below says which it uses.

| Client | Status | Reaches Cairn through | Hooks |
|---|---|---|---|
| Claude Code | Supported. The plugin is M5 work and is not shipped | `cairn mcp`, registered by hand | Yes, three, installed by `cairn enable` |
| opencode | EXPERIMENTAL. No real run has confirmed it | `cairn mcp`, registered by `cairn enable` | No |
| pi | EXPERIMENTAL. No real run has confirmed it | `cairn mcp`, registered by `cairn enable` | No |
| Any agent with a shell | Supported | The CLI verbs, [CLI-Reference](CLI-Reference) | Not applicable |

## Before you connect any client

- **A server** that the client machine can reach, and a **project** with a `.cairn.toml` that names it. [Install-Local](Install-Local) covers both.
- **One agent key for each client.** Make it on the server with `cairn key add --scope agent`, and give it to the client. Do not share one key between clients: revoking it stops all of them at once. [Operations](Operations) covers keys.
- **`cairn` on the PATH** of the client's shell and of the client itself. The hooks and the MCP entry both run `cairn` by name, unless you give the full path.
- **The key on the client,** in `~/.config/cairn/client.env`, not in the MCP entry. A key written into an MCP entry or a settings file is written to disk in that file.

## Claude Code

Claude Code connects in three steps: the hooks, the MCP entry and the check.

### Step 1: the hooks

From the project folder:

```bash
cairn enable --agent claude-code --dry-run
cairn enable --agent claude-code
```

`--dry-run` prints the three hooks that would be added, and writes nothing. The real run adds them to `.claude/settings.local.json` in the project, and keeps every other entry in that file. Before it rewrites a file, it copies the original to the user's cache folder and prints the path.

The three hooks:

| Event | Matcher | Command | What it does in Claude Code |
|---|---|---|---|
| `SessionStart` | `startup\|resume\|clear\|compact` | `cairn hook session-start` | Prints the active task, its steps and its directives. After `/compact`, the output is added to the agent's context. This is what carries a task across compaction |
| `PreCompact` | none | `cairn hook pre-compact` | Prints the same block. Claude Code does not add this hook's output to the context, so it does nothing there. It is installed for the design (D15), and the operator decides whether to keep it |
| `Stop` | none | `cairn hook stop` | Prints a warning when a step of the active task is active with no summary. Otherwise it prints nothing |

A hook never blocks the session. It exits 0 even when it has nothing to say.

### Step 2: the MCP entry

`enable` does not register the MCP server. Register it with Claude Code:

```bash
claude mcp add --scope user cairn -- "$(command -v cairn)" mcp
claude mcp list
claude mcp get cairn
```

`--scope user` makes the entry available in every project. `--scope local` is Claude Code's default scope. `--scope project` writes a `.mcp.json` into the project, which holds the absolute path of your binary: do not commit that file. The `--` separates the command from its options. The path is the one `command -v cairn` prints, which is absolute, so Claude Code does not depend on the PATH to start it.

`claude mcp add --help` in Claude Code 2.1.311 gives this syntax. The commands were not run end to end against Cairn when this page was written.

`cairn mcp` finds the server and the key the same way every other command does. It finds `.cairn.toml` from `CLAUDE_PROJECT_DIR` when Claude Code sets it, and from its working directory otherwise. How Claude Code sets these for an MCP server is the M5 check (see [Checks](Checks)), and this page does not claim it.

### Step 3: the check

```bash
cairn doctor --agent claude-code
```

This lists the hooks in the Claude Code settings files and runs each hook once against the server, so the output shows what each hook would print. The first read of a project the server has not seen creates the project's row. `doctor` warns when a hook is not installed, and tells you to run `cairn enable --agent claude-code`.

Restart Claude Code in the project after you change its MCP entry or its hooks.

### What is verified about `/compact`

The `SessionStart` hook carries a task across `/compact`. The M4 hand check, section 3, was run on 2026-11-06 with a task started before `/compact` and continued after it. It passed by operator ruling. The hook's output asks the agent to call `task_current` for the brief, and the agent made that call in the run, which the guide's own rule counts against the check. The operator ruled it a pass, and issue #206 asks whether the hook text should change. That is open. A later operator decision can reverse the ruling, and this page will follow it.

Sections 4 and 5 of the guide, for opencode and pi, have not been run.

## Agents with only a shell

An agent that cannot use MCP calls the CLI verbs. Set `CAIRN_AGENT` to the agent's role, so the write verbs know the caller:

```bash
export CAIRN_AGENT=main
cairn prime
cairn task start --title "Add the export flag" --goal "The export command takes a format flag." --json
cairn step add <task-id> --title "Write the change"
cairn step update <task-id> 1 --action start
cairn step update <task-id> 1 --action finish --summary "Written and tested"
echo '{"lessons":[],"mistakes":[]}' | cairn task close <task-id> --outcome done --summary "Done" --no-lessons-reason "Nothing new" --json-input
```

`<task-id>` is the `id` that `task start --json` prints. The last command reads its lessons and mistakes as one JSON object from standard input. An empty `lessons` list needs `--no-lessons-reason`, and `mistakes` may be `[]`. Which of these a close needs depends on the project's close policy: the refusal names what is missing. The verbs and their flags are in [CLI-Reference](CLI-Reference).

`cairn prime` prints the session-start view, the same content as the session-start hook, for an agent that will not run hooks. The M4 check passed for this path by its automated exit test: an agent with only bash completes and closes a task through the CLI.

## opencode (EXPERIMENTAL)

```bash
cairn enable --agent opencode --dry-run
cairn enable --agent opencode
cairn doctor --agent opencode
```

`enable --agent opencode` writes into the project:

- `opencode.json`, with the Cairn MCP server under `mcp`:

  ```json
  "mcp": {"cairn": {"type": "local", "command": ["cairn", "mcp"], "timeout": 30000}}
  ```

- `.cairn/instructions.md`, which holds the task loop for the agent, and is listed in the `instructions` of `opencode.json`.

It refuses when an `opencode.jsonc` exists. An `opencode.json` that is not plain JSON, such as one with comments, is refused as well. The instruction file is replaced when it differs from Cairn's text, but only when it starts with Cairn's marker line. A file without the marker is yours, and is left as it is.

No real OpenCode run has confirmed that OpenCode reads `.cairn/instructions.md` from that path. Every line `enable` prints says EXPERIMENTAL.

## pi (EXPERIMENTAL)

```bash
cairn enable --agent pi --dry-run
cairn enable --agent pi
cairn doctor --agent pi
```

`enable --agent pi` writes into the project:

- `.pi/mcp.json`, with the Cairn MCP server:

  ```json
  "mcpServers": {"cairn": {"command": "cairn", "args": ["mcp"], "exposure": "direct"}}
  ```

- A block in `AGENTS.md`, between two marker lines, with the task loop. Text outside the block is never changed. A block Cairn wrote earlier is updated to the current text. Any other marker text in `AGENTS.md` is refused, and you fix the markers by hand.

pi reads `.pi/mcp.json` only after the project is trusted. No real pi run has confirmed the file, the marker block or the exposure setting. Every line `enable` prints says EXPERIMENTAL.

## What `enable` writes

`cairn enable --agent <name>` is the one command that writes client configuration. The table gives each target.

| Agent | Writes | Does not write |
|---|---|---|
| `claude-code` | Three hooks in `.claude/settings.local.json` | An MCP entry. Register it with `claude mcp add` (above) |
| `opencode` (EXPERIMENTAL) | The Cairn server in `opencode.json` under `mcp`, the instruction file `.cairn/instructions.md`, and its listing in `instructions` | The key. The key comes from the environment or `client.env` |
| `pi` (EXPERIMENTAL) | The Cairn server in `.pi/mcp.json`, and the task-loop block in `AGENTS.md` | The key |

Every `enable` run with `--dry-run` writes nothing and says so. Every run that changes a file copies the original first, to the user's cache folder, and prints where the copy is. Nothing removes those copies. `enable` also warns when `cairn` is not on the PATH of its shell.

## Keys for clients

Give each client its own agent key, made on the server:

```bash
cairn key add --db <file> --name <client-name> --scope agent
```

The client reads the key from `CAIRN_API_KEY` or from `~/.config/cairn/client.env`, as [Install-Local](Install-Local) describes. An agent key makes the 15 tools' calls and the CLI's verbs, and it cannot reach the operator routes. A viewer key can call the seven read tools and nothing else. An operator key can make every agent call, and also makes the decisions in the web UI. Use an agent key for an agent.
