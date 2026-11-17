# Install the Claude Code plugin

_Status as of 2026-11-17: this guide is written for the M5 plugin. The plugin is built on the `m5-plugin` branch and is not on `main` yet, so the GitHub install in section 5 works only after M5 merges. The guide has not been run end to end on a clean machine, and it has not been timed. The exact tool names Claude Code shows are not confirmed in a live session (issue #26)._

This guide takes a developer from a clean machine to a recorded task, with no other instruction file. Run the commands in order. Replace each placeholder in angle brackets before you run its line. The first build is the slow part.

The plugin is the recommended route for Claude Code. It brings the MCP server and the three hooks, so you do not run `cairn enable` or `claude mcp add`. It asks for no key and no server: the key comes from `~/.config/cairn/client.env` and the server from the project's `.cairn.toml` (D36). [Connecting-Agents](Connecting-Agents) describes the route without the plugin, and section 8 here covers using both by mistake.

## 1. What the machine needs

- **Go 1.26.** The version is the one in [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod), which declares `go 1.26.0` and `toolchain go1.26.10`. There are no release archives yet, so `cairn` is built from source. No C toolchain is needed.
- **git.** To clone the repository. `make build` reads the version from git.
- **Claude Code.** The plugin is validated in CI with Claude Code 2.1.326. Section 5 has one command that needs 2.1.275 or later. Check your version with `claude --version`.
- **A server the machine can reach.** Either one you run on this machine (section 3 starts it on loopback), or a shared server whose operator gives you its URL and an agent key.
- **A project folder.** Any folder you want Cairn to record. Use a project folder, not the clone of this repository. Section 4 writes its `.cairn.toml`.
- **An agent key.** Section 3 makes one on a local server. On a shared server, the operator gives you one.

## 2. Install `cairn`

Run these in your checkout of the repository. They are the commands from [QUICK-INSTALL](https://github.com/djm56/cairn-memory/blob/main/docs/QUICK-INSTALL.md), section 1:

```bash
cd <repo>
git switch main && git pull --ff-only
make build && bin/cairn version
```

Expect `cairn` followed by a short commit hash. If the hash ends in `-dirty`, the checkout has local changes: deal with them first. Build with `make build`, not `go build`, which prints `cairn dev`.

Before M5 merges, `main` has no plugin. In a local checkout that carries the plugin branch, skip the `git switch main` part and stay on that branch. Section 5 reads the plugin from your checkout.

Then install the binary:

```bash
mkdir -p ~/.local/bin && install -m 0755 bin/cairn ~/.local/bin/cairn
command -v cairn
```

`command -v cairn` prints a path. If it prints nothing, add `~/.local/bin` to your PATH and open a new terminal. The plugin's launcher also looks in `~/.local/bin`, so the plugin finds `cairn` either way. Your shell needs the PATH for the `cairn` commands in this guide.

## 3. Make the key and start a server

Keys are made on the server. This section sets up a server on this machine. On a shared server, skip the key and server lines, and store the key the operator gave you with the block below.

```bash
mkdir -p ~/cairn-data
cairn key add --db ~/cairn-data/cairn.db --name laptop --scope agent
```

The key prints once. Copy it now. A name is never reused, so a second run with `laptop` fails. The web UI needs an operator key (`--scope operator`); this guide does not.

Store the key in a file only you can read. Paste the key at the prompt, so it stays out of your shell history:

```bash
mkdir -p -m 700 ~/.config/cairn && chmod 700 ~/.config/cairn
printf 'API key: ' >&2; read -rs K; echo >&2
[ -n "$K" ] && (umask 077 && set -C && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
ls -l ~/.config/cairn/client.env
```

The file holds one line, `CAIRN_API_KEY=cairn_...`. The `ls` line should show `-rw-------`. An empty paste writes nothing, so run the block again. If the write fails because the file exists, the file already holds a key: check it before you replace the key. Do not put the key in `.cairn.toml` or in an MCP entry.

Start the server in a second terminal, and leave it running there:

```bash
cairn serve --db ~/cairn-data/cairn.db
```

Back in the first terminal, check that it answers:

```bash
curl -sS http://127.0.0.1:8788/healthz
```

Expect `{"status":"ok"}`. The server listens on `127.0.0.1:8788`. Stop it later with Ctrl-C in its terminal.

## 4. Point the project at the server

In the project folder, not the clone:

```bash
cd <project>
cairn init --project <name> --server http://127.0.0.1:8788
cairn doctor
```

The project name is lowercase letters and digits in hyphen-separated groups, at most 64 characters. `cairn init` writes `.cairn.toml` and does not contact the server. The file holds no key, so commit it. On a shared server, use its URL in place of `http://127.0.0.1:8788`.

`cairn doctor` prints five lines, `binary`, `project`, `server`, `key` and `reach`. Each should say `ok`. The `reach` line calls the server, so the server in section 3 must be running. If a line says `fail`, [Install-Local](Install-Local) section 6 says what each line checks.

## 5. Install the plugin

The repository is a Claude Code marketplace. Its `.claude-plugin/marketplace.json` lists one plugin, `cairn`, and the marketplace is named `cairn-memory`. The install id is therefore `cairn@cairn-memory`.

Choose the route that matches your checkout. Both routes give the same plugin.

### 5a. From GitHub, once M5 is on `main`

Start Claude Code in the project folder with `claude`, then type these two lines in the session:

```text
/plugin marketplace add djm56/cairn-memory
/plugin install cairn@cairn-memory
```

The install line opens the plugin's details. Choose a scope: user scope applies to every project on this machine, and local scope to this project only. The last line of the install summary says whether the plugin is active (`Plugin is now active.`) or needs `/reload-plugins`.

With Claude Code 2.1.275 or later, one line adds the marketplace and opens the plugin's details:

```text
/plugin install cairn --marketplace djm56/cairn-memory
```

From a shell instead, run these two commands. The shell install uses user scope unless you pass `--scope project` or `--scope local`:

```bash
claude plugin marketplace add djm56/cairn-memory
claude plugin install cairn@cairn-memory
```

Until M5 is on `main`, this route finds no `cairn` plugin. Use 5b.

### 5b. From a local checkout

Use this route for development, and before M5 is on `main`. Give the path to your checkout. It can be absolute, or relative and starting with `./` or `../`:

```text
/plugin marketplace add /absolute/path/to/cairn-memory
/plugin install cairn@cairn-memory
```

From a shell, the same two commands are `claude plugin marketplace add /absolute/path/to/cairn-memory` and `claude plugin install cairn@cairn-memory`.

Claude Code reads the plugin's files from the checkout, so an edit there takes effect at the next session start, or after `/reload-plugins` in a session. The checkout must hold the plugin: `plugins/claude-code` on the branch you have checked out.

The plugin asks for no key and no server. Section 3 and section 4 provide both.

## 6. Check the install

Run these from the project folder. Start a new session with `claude` first.

1. In a shell, `claude plugin list` lists `cairn@cairn-memory` with its status.
2. In the session, type `/` and look for `/cairn:status`. Run it. It prints the project, the server and the active task, or `task none active`.
3. In the session, run `/mcp`. The list should include `cairn`.
4. The SessionStart hook adds a block to Claude's context at the start of each session. With no active task the block is one line: `Cairn: no task is active in <project>.` You can print the same text yourself from the project folder with `cairn hook session-start`.

Whether Claude Code shows the block on screen is not confirmed in a live session (issue #26). The hook's output does reach Claude's context, as the Claude Code hooks reference describes it.

## 7. Record a task

### The tool names

The plugin's skill teaches Claude this loop. It calls these tools by their short names: `recall`, `task_start`, `step_add`, `step_update`, `event_log`, `learning_add`, `task_close`, and `task_current` when the session-start block is missing. [MCP-Tools](MCP-Tools) lists all 15.

> **Not yet confirmed:** the exact names Claude Code shows for these tools are confirmed in a live session under issue #26. This guide uses the short names only.

### Walk through one task in a terminal

This is the same loop as the skill, run from the shell with the CLI verbs in [CLI-Reference](CLI-Reference). It needs no session. Run it in the project folder:

```bash
cd <project>
export CAIRN_AGENT=main
cairn recall --goal "Record one task from the install guide" --agent main
cairn task start --title "Try Cairn" --goal "Record one task from the install guide." --json
```

The JSON that `task start` prints has an `id` field. Copy its value and set it once:

```bash
export TASK=<task-id>
cairn step add "$TASK" --title "Write the first note"
cairn step update "$TASK" 1 --action start
cairn step update "$TASK" 1 --action finish --summary "Written"
cairn event log "$TASK" --kind note --text "Recorded from the install guide"
echo '{"lessons":[],"mistakes":[]}' | cairn task close "$TASK" --outcome done --summary "Install guide walked through" --no-lessons-reason "Nothing new" --json-input
cairn status
```

The last line prints `task none active`, because the task is closed. `cairn task list` shows the task among the project's tasks. `CAIRN_AGENT` names you as the caller on every write, so the verbs need no `--agent`.

To record a task from inside Claude Code, ask Claude to start a Cairn task for a small piece of work. The skill tells it the loop. Check the result in a terminal with `cairn task current`.

## 8. If you ran `cairn enable` first

The plugin and `cairn enable --agent claude-code` are alternatives (D34). The plugin carries the same three hooks, so keep one route.

- **Duplicate hooks.** `cairn doctor --agent claude-code` warns when the plugin is enabled and cairn hooks are also in a settings file. Its `plugin` line names each file that holds one: the project's `.claude/settings.local.json` or `.claude/settings.json`, or the user's `~/.claude/settings.json`. Without the warning, each hook runs twice.
- **Removing enable's hooks.** `cairn enable` has no flag to remove them. Edit the files that `doctor` names by hand. Under `hooks`, delete the entries whose command is `cairn hook session-start`, `cairn hook pre-compact` or `cairn hook stop`, and keep every other entry. Before `enable` changed a file, it copied the original to the cache folder it names when it ran. Keep that copy if you want the file back.
- **A manual MCP entry.** If you also added cairn with `claude mcp add`, remove it with `claude mcp remove cairn`, adding `-s <scope>` if you used one. The plugin brings its own MCP server. Whether Claude Code shows two servers while both entries exist is not confirmed in a live session (issue #26).

After the edits, `cairn doctor --agent claude-code` should show no `plugin` warning.

## 9. Troubleshooting

**Exit 127, `cairn` not found.** The plugin's launcher, `cairn-launch`, looks for `cairn` on PATH and then in `~/.local/bin`. When it finds neither, it prints one line on stderr and exits 127:

```text
cairn-launch: cairn is not on PATH or in ~/.local/bin. Install it: in your cairn checkout, run make build, then mkdir -p ~/.local/bin && install -m 0755 bin/cairn ~/.local/bin/cairn (docs/QUICK-INSTALL.md, section 1)
```

Run section 2, then check with `command -v cairn`. How Claude Code shows a server or hook that exits 127 is not confirmed in a live session (issue #26).

**`no project: run cairn init in the project, or set CAIRN_PROJECT`.** The folder has no `.cairn.toml` above it. Run section 4 in the project folder, or start Claude Code from that folder.

**A `doctor` line says `fail`.** The `project` line means no `.cairn.toml`. The `server` line means no server URL. The `key` line means no key in `CAIRN_API_KEY` or `client.env`. The `reach` line means the server does not answer as Cairn, or refuses the key. Each fix is in sections 3 and 4.

**No Cairn block at session start.** The skill's rule is that a missing block means the hook did not run or could not read the task. Run `cairn hook session-start` in the project folder. It prints the block, or the one-line no-task message. If it prints nothing, it says why on stderr: fix that cause, then run `cairn doctor`.

**`task start` is refused.** A task is already active. `cairn status` names it. Close it with `cairn task close <task-id> --outcome done` (or `failed`), supplying the input the close asks for if it is refused, then start the new one. [CLI-Reference](CLI-Reference) gives the close flags.

## Where to read next

- [Quick-Start](Quick-Start): one machine, the whole path, with a "you should see" line for each step.
- [Install-Local](Install-Local): the same install in detail, with the files and environment variables.
- [Connecting-Agents](Connecting-Agents): the route without the plugin, and the other clients.
- [CLI-Reference](CLI-Reference) and [MCP-Tools](MCP-Tools): every verb and every tool.
