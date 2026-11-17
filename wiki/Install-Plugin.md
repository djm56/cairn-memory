# Install the Claude Code plugin

_Status as of 2026-11-17: this guide is written for the M5 plugin. The plugin is built on the `m5-plugin` branch and is not on `main` yet, so the GitHub install in section 5a works only after M5 merges. Live check on 2026-11-17 (issue #26), Claude Code 2.1.311, with the plugin loaded from the checkout by `--plugin-dir`. Confirmed: the plugin's server name and tool names, the `/cairn:init` and `/cairn:status` commands, the session-start block with and without a task, a task recorded from inside a session through the plugin's tools, that a step must be started before it can be finished, and that a user-scope `cairn` entry adds a second server. Not confirmed: the marketplace install in section 5 (the probe did not use it), the pre-compact hook's effect, how Claude Code shows a server or hook that exits 127, and what `/mcp` prints. The guide has not been run end to end on a clean machine, and it has not been timed._

This guide takes a developer from a clean machine to a recorded task, with no other instruction file. Run the commands in order. Replace each placeholder in angle brackets before you run its line. The first build is the slow part.

The plugin is the recommended route for Claude Code. It brings the MCP server and the three hooks, so you do not run `cairn enable` or `claude mcp add`. It asks for no key and no server: the key comes from `~/.config/cairn/client.env` and the server from the project's `.cairn.toml` (D36). [Connecting-Agents](Connecting-Agents) describes the route without the plugin, and section 8 here covers using both by mistake.

## 1. What the machine needs

- **Go 1.26.** The version is the one in [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod), which declares `go 1.26.0` and `toolchain go1.26.10`. There are no release archives yet, so `cairn` is built from source. No C toolchain is needed.
- **git.** To clone the repository. `make build` reads the version from git.
- **Claude Code.** The one-line install in section 5a needs 2.1.275 or later. The live check ran 2.1.311. Check your version with `claude --version`. CI validates the plugin with Claude Code 2.1.326; that is a check on the repository, and you do not need that version.
- **A server the machine can reach.** Either one you run on this machine (section 3 starts it on loopback), or a shared server whose operator gives you its URL and an agent key.
- **A project folder.** Any folder you want Cairn to record. Use a project folder, not the clone of this repository. Section 4 writes its `.cairn.toml`.
- **An agent key.** Section 3 makes one on a local server. On a shared server, the operator gives you one.

## 2. Install `cairn`

Clone the repository. If you already have a checkout, run the `cd` line and the lines after it in that checkout. These are the commands from [QUICK-INSTALL](https://github.com/djm56/cairn-memory/blob/main/docs/QUICK-INSTALL.md), section 1:

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
git switch main && git pull --ff-only
make build && bin/cairn version
```

Expect `cairn` followed by a short commit hash. If the hash ends in `-dirty`, the checkout has local changes: deal with them first. Build with `make build`, not `go build`, which prints `cairn dev`.

Before M5 merges, `main` has no plugin. Use the `m5-plugin` branch instead. Once that branch is pushed, a fresh clone runs `git switch m5-plugin` in place of `git switch main`. Until then, use a local checkout that already carries the branch, and stay on it. Section 5b reads the plugin from that checkout.

Then install the binary:

```bash
mkdir -p ~/.local/bin && install -m 0755 bin/cairn ~/.local/bin/cairn
command -v cairn
```

`command -v cairn` prints a path. If it prints nothing, add `~/.local/bin` to your PATH and open a new terminal. The plugin's launcher also looks in `~/.local/bin`, so the plugin finds `cairn` either way. Your shell needs the PATH for the `cairn` commands in this guide. The launcher runs the first `cairn` on PATH, so section 8 covers what to do when you update the checkout.

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

Until M5 is on `main`, use 5b. After M5 is on `main`, use 5a. Both routes give the same plugin. The live check loaded the plugin with `--plugin-dir`, not through the marketplace, so the marketplace commands in both routes are not live-checked.

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
2. In the session, type `/` and look for `/cairn:status`, which the live check's session listed. Run it. It prints the project, the server and the active task, or `task none active`.
3. The session's start-up record names the plugin's server `plugin:cairn:cairn`, and in the live check it was connected. Its 15 tools are named `mcp__plugin_cairn_cairn__<tool>`. The live check did not capture `/mcp` output, so this guide does not say what `/mcp` prints. Section 7a is the check that counts.
4. The SessionStart hook adds a block to Claude's context at the start of each session. With no active task the block is one line: `Cairn: no task is active in <project>.` With a task it is five lines, and the last one is `The block above is the task to continue.` You can print the same text yourself from the project folder with `cairn hook session-start`.

The live check's log records the block as the output of the SessionStart hook, with exit code 0. Whether Claude Code shows the block on screen was not checked (issue #26). The Claude Code hooks reference says the output reaches Claude's context; the live check did not test that.

## 7. Record a task

### The tool names

The plugin's skill teaches Claude this loop, with the tools under their short names: `recall`, `task_start`, `step_add`, `step_update`, `event_log`, `learning_add`, `task_close`, and `task_current` when the session-start block is missing. [MCP-Tools](MCP-Tools) lists all 15. In the live check's session each one was named `mcp__plugin_cairn_cairn__<short name>`, so `task_start` appeared as `mcp__plugin_cairn_cairn__task_start`. The skill and this guide use the short names.

### 7a. Record a task in a session (the check that counts)

This check runs the plugin from inside Claude Code and reads the result from a terminal. It is the check the install must pass. Run it in the project folder, after section 6.

1. Start Claude Code in the project folder with `claude`.
2. Ask Claude, in your own words, to record one small piece of work as a Cairn task. Give your agent name, so Claude passes it on every write. For example:

   ```text
   Start a Cairn task for one small piece of work you can finish in this session. Use <your-agent-name> as the agent on every Cairn write. Add one step, start it, do the work, finish it with a summary, and close the task with a one-line outcome.
   ```

   Claude uses the plugin's tools for this: `task_start`, `step_add`, `step_update` to start and finish the step, and `task_close`. A step must be started before it can be finished, and Claude does that. If the close is refused, the refusal names what is missing, and the skill tells Claude how to answer it.

3. When Claude has closed the task, run these in a terminal in the project folder:

   ```bash
   cairn task list
   cairn task get <task-id> --section steps
   ```

   Replace `<task-id>` with the first word on the first line that `task list` prints.

The check passes when both of these hold:

- `cairn task list` shows the task with status `done`, one line, for example:

  ```text
  task-20261117-01  done  normal  Add one note to the scratch file
  ```

- `cairn task get <task-id> --section steps` lists the step as `[done]`, with the summary Claude wrote.

If the task is missing, the session and the terminal are on different projects or servers. Run `cairn status` in the same folder and compare its project and server lines with the `.cairn.toml` file.

### 7b. Walk through one task in a terminal (checks the server and the CLI, not the plugin)

This walk-through runs the server and the CLI from a shell, with the CLI verbs in [CLI-Reference](CLI-Reference). It does not start Claude Code, so it checks the server and the CLI, not the plugin. It is useful for a shell-only agent, and for a quick check of the server, but it does not count for the install check in 7a. Run it in the project folder:

```bash
cd <project>
export CAIRN_AGENT=<your-agent-name>
cairn recall --goal "Record one task from the install guide" --agent <your-agent-name>
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

The last line prints `task        none active` (eight spaces between `task` and `none`), because the task is closed. The first two lines name the project and the server. `cairn task list` shows the task among the project's tasks.

On the CLI, `CAIRN_AGENT` names you as the caller on every write that leaves `--agent` out, so these verbs need no `--agent`. This holds for the CLI only. The plugin's MCP writes store the `agent` argument that Claude passes, and do not read `CAIRN_AGENT`.

## 8. If you ran `cairn enable` first

The plugin and `cairn enable --agent claude-code` are alternatives (D34). The plugin carries the same three hooks, so keep one route.

- **Duplicate hooks.** `cairn doctor --agent claude-code` warns when the plugin is enabled and cairn hooks are also in a settings file. Its `plugin` line names each file that holds one: the project's `.claude/settings.local.json` or `.claude/settings.json`, or the user's `~/.claude/settings.json`. Without the warning, each hook runs twice.
- **Removing enable's hooks.** `cairn enable` has no flag to remove them. Edit the files that `doctor` names by hand. Under `hooks`, delete the entries whose command is `cairn hook session-start`, `cairn hook pre-compact` or `cairn hook stop`, and keep every other entry. Before `enable` changed a file, it copied the original to the cache folder it names when it ran. Keep that copy if you want the file back.
- **A manual MCP entry.** If you also added cairn with `claude mcp add`, the session shows two servers: the plugin's `plugin:cairn:cairn` and your `cairn`. In the live check both were connected, each with 15 tools: the plugin's as `mcp__plugin_cairn_cairn__<tool>`, the manual one as `mcp__cairn__<tool>`. The two do not merge. Once the plugin is installed, remove the manual entry with `claude mcp remove cairn -s user`, or with the scope you used. The plugin brings its own MCP server.
- **An old `cairn` on PATH.** The plugin's launcher runs the first `cairn` it finds on PATH, and only then the one in `~/.local/bin`. A `cairn` that comes earlier on PATH can be an older build than the checkout the plugin comes from. When you update the checkout, rebuild and reinstall the binary: `make build`, then `install -m 0755 bin/cairn ~/.local/bin/cairn`. `command -v cairn` shows the file the launcher will run first.

After the edits, `cairn doctor --agent claude-code` should show no `plugin` warning.

## 9. Troubleshooting

**Exit 127, `cairn` not found.** The plugin's launcher, `cairn-launch`, looks for `cairn` on PATH and then in `~/.local/bin`. When it finds neither, it prints one line on stderr and exits 127:

```text
cairn-launch: cairn is not on PATH or in ~/.local/bin. Install it: in your cairn checkout, run make build, then mkdir -p ~/.local/bin && install -m 0755 bin/cairn ~/.local/bin/cairn (docs/QUICK-INSTALL.md, section 1)
```

Run section 2, then check with `command -v cairn`. The live check did not reproduce this case, because `cairn` was installed on its machine. How Claude Code shows a server or hook that exits 127 is not confirmed (issue #26).

**`no project: run cairn init in the project, or set CAIRN_PROJECT`.** The folder has no `.cairn.toml` above it. Run section 4 in the project folder, or start Claude Code from that folder.

**A `doctor` line says `fail`.** The `project` line means no `.cairn.toml`. The `server` line means no server URL. The `key` line means no key in `CAIRN_API_KEY` or `client.env`. The `reach` line means the server does not answer as Cairn, or refuses the key. Each fix is in sections 3 and 4.

**No Cairn block at session start.** The skill's rule is that a missing block means the hook did not run or could not read the task. Run `cairn hook session-start` in the project folder. It prints the block, or the one-line no-task message. If it prints nothing, it says why on stderr: fix that cause, then run `cairn doctor`.

**`task start` is refused.** A task is already active. `cairn status` names it. Close it, then start the new one. The close needs its input: a summary, lessons or a reason for having none, and mistakes. That is why the short form, `cairn task close <task-id> --outcome done`, is refused. Use the full close from section 7b instead. Its `--json-input` with `{"lessons":[],"mistakes":[]}` on standard input, together with `--no-lessons-reason`, is what satisfies it. [CLI-Reference](CLI-Reference) gives the close flags.

## Where to read next

- [Quick-Start](Quick-Start): one machine, the whole path, with a "you should see" line for each step.
- [Install-Local](Install-Local): the same install in detail, with the files and environment variables.
- [Connecting-Agents](Connecting-Agents): the route without the plugin, and the other clients.
- [CLI-Reference](CLI-Reference) and [MCP-Tools](MCP-Tools): every verb and every tool.
