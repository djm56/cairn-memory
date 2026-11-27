# Install the Claude Code plugin

_Status as of 2026-11-24: M5 is merged to `main` (PR #229, merge commit d3ff98f, 2026-11-21), and the plugin is meant to install from GitHub through section 5a (not yet live-checked). Live check on 2026-11-17 (issue #26), Claude Code 2.1.311, with the plugin loaded from the checkout by `--plugin-dir`. Confirmed: the plugin's server name and tool names, the `/cairnbook:init` and `/cairnbook:status` commands, the session-start block with and without a task, a task recorded from inside a session through the plugin's tools, that a step must be started before it can be finished, and that a user-scope `cairnbook` entry adds a second server. Not confirmed: the marketplace install in section 5 (the probe did not use it), the pre-compact hook's effect, how Claude Code shows a server or hook that exits 127, and what `/mcp` prints. The guide has not yet been run end to end on a clean machine, and it has not been timed. That run is the pending M5 exit check, section 7a._

This guide takes a developer from a clean machine to a recorded task, with no other instruction file. Run the commands in order. Replace each placeholder in angle brackets before you run its line. The first build is the slow part.

The plugin is the recommended route for Claude Code. It brings the MCP server and the three hooks, so you do not run `cairnbook enable` or `claude mcp add`. It asks for no key and no server: the key comes from `~/.config/cairnbook/client.env` and the server from the project's `.cairnbook.toml` (D36). [Connecting-Agents](Connecting-Agents) describes the route without the plugin, and section 8 here covers using both by mistake.

## 1. What the machine needs

- **Go 1.26.** The version is the one in [go.mod](https://github.com/djm56/cairn-memory/blob/main/go.mod), which declares `go 1.26.0` and `toolchain go1.26.10`. There are no release archives yet, so `cairnbook` is built from source. No C toolchain is needed.
- **git.** To clone the repository. `make build` reads the version from git.
- **Claude Code.** The one-line install in section 5a needs 2.1.275 or later, the version the [Claude Code plugin docs](https://code.claude.com/docs/en/discover-plugins) give for `/plugin install` with `--marketplace`. The live check ran 2.1.311. Check your version with `claude --version`. CI validates the plugin with Claude Code 2.1.326; that is a check on the repository, and you do not need that version.
- **A server the machine can reach.** Either one you run on this machine (section 3 starts it on loopback), or a shared server whose operator gives you its URL and an agent key.
- **A project folder.** Any folder you want Cairnbook to record. Use a project folder, not the clone of this repository. Section 4 writes its `.cairnbook.toml`.
- **An agent key.** Section 3 makes one on a local server. On a shared server, the operator gives you one.

## 2. Install `cairnbook`

Run this block in the folder where you keep your code. The first line creates a `cairn-memory` folder there, and the `cd` line enters it. Because that `cd` is relative, it works only straight after the clone, from the folder the clone was made in. The commands follow [QUICK-INSTALL](https://github.com/djm56/cairn-memory/blob/main/docs/QUICK-INSTALL.md), section 1.

```bash
git clone https://github.com/djm56/cairn-memory.git
cd cairn-memory
git switch main && git pull --ff-only
make build && bin/cairnbook version
mkdir -p ~/.local/bin && install -m 0755 bin/cairnbook ~/.local/bin/cairnbook
command -v cairnbook
```

If you already have a checkout, skip the `git clone` and `cd cairn-memory` lines. Run `cd <path-to-your-checkout>` in their place, with the path to the checkout's top folder, from any directory. Then run the rest of the block.

If you switch branches after building, run `make build` and the install line again. The binary is from the branch it was built on.

Expect `cairnbook` followed by a short commit hash. If the hash ends in `-dirty`, the checkout has local changes: deal with them first. Build with `make build`, not `go build`, which prints `cairnbook dev`.

`command -v cairnbook` prints a path. If it prints nothing, add `~/.local/bin` to your PATH and open a new terminal. The plugin's launcher also looks in `~/.local/bin`, so the plugin finds `cairnbook` either way. Your shell needs the PATH for the `cairnbook` commands in this guide. The launcher runs the first `cairnbook` on PATH, so section 8 covers what to do when you update the checkout.

## 3. Make the key and start a server

Keys are made on the server. This section sets up a server on this machine. On a shared server, skip the key and server lines, and store the key the operator gave you with the block below.

```bash
mkdir -p ~/cairnbook-data
cairnbook key add --db ~/cairnbook-data/cairnbook.db --name laptop --scope agent
```

The key prints once. Copy it now. A name is never reused, so a second run with `laptop` fails. The web UI needs an operator key (`--scope operator`); this guide does not.

Store the key in a file only you can read. Paste the key at the prompt, so it stays out of your shell history:

```bash
mkdir -p -m 700 ~/.config/cairnbook && chmod 700 ~/.config/cairnbook
printf 'API key: ' >&2; read -rs K; echo >&2
[ -n "$K" ] && (umask 077 && set -C && printf 'CAIRNBOOK_API_KEY=%s\n' "$K" > ~/.config/cairnbook/client.env); unset K
ls -l ~/.config/cairnbook/client.env
```

The file holds one line, `CAIRNBOOK_API_KEY=cairnbook_...`. The `ls` line should show `-rw-------`. An empty paste writes nothing, so run the block again. If the write fails because the file exists, the file already holds a key: check it before you replace the key. Do not put the key in `.cairnbook.toml` or in an MCP entry.

Start the server in a second terminal, and leave it running there:

```bash
cairnbook serve --db ~/cairnbook-data/cairnbook.db
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
cairnbook init --project <name> --server http://127.0.0.1:8788
cairnbook doctor
```

The project name is lowercase letters and digits in hyphen-separated groups, at most 64 characters. `cairnbook init` writes `.cairnbook.toml` and does not contact the server. The file holds no key, so commit it. On a shared server, use its URL in place of `http://127.0.0.1:8788`.

`cairnbook doctor` prints five lines, `binary`, `project`, `server`, `key` and `reach`. Each should say `ok`. The `reach` line calls the server, so the server in section 3 must be running. If a line says `fail`, [Install-Local](Install-Local) section 6 says what each line checks.

## 5. Install the plugin

The repository is a Claude Code marketplace. Its `.claude-plugin/marketplace.json` lists one plugin, `cairnbook`, and the marketplace is named `cairnbook`. The install id is therefore `cairnbook@cairnbook`.

Use 5a to install the plugin. Use 5b only to develop the plugin, from a checkout. Both routes give the same plugin. The marketplace commands in 5a are not live-checked: the live check on 2026-11-17 loaded the plugin with `--plugin-dir`, not through the marketplace.

### 5a. From GitHub

Start Claude Code in the project folder with `claude`, then type these two lines in the session:

```text
/plugin marketplace add djm56/cairn-memory
/plugin install cairnbook@cairnbook
```

The install line opens the plugin's details. Choose a scope: user scope applies to every project on this machine, and local scope to this project only. The last line of the install summary says whether the plugin is active (`Plugin is now active.`) or needs `/reload-plugins`.

With Claude Code 2.1.275 or later, the version the [Claude Code plugin docs](https://code.claude.com/docs/en/discover-plugins) give for `--marketplace`, one line adds the marketplace and opens the plugin's details:

```text
/plugin install cairnbook --marketplace djm56/cairn-memory
```

From a shell instead, run these two commands. The shell install uses user scope unless you pass `--scope project` or `--scope local`:

```bash
claude plugin marketplace add djm56/cairn-memory
claude plugin install cairnbook@cairnbook
```

### 5b. From a local checkout

Use this route to develop the plugin. It loads the plugin from your checkout, so your edits to the plugin take effect. Give the path to your checkout. It can be absolute, or relative and starting with `./` or `../`:

```text
/plugin marketplace add /absolute/path/to/cairn-memory
/plugin install cairnbook@cairnbook
```

From a shell, the same two commands are `claude plugin marketplace add /absolute/path/to/cairn-memory` and `claude plugin install cairnbook@cairnbook`.

Claude Code reads the plugin's files from the checkout, so an edit there takes effect at the next session start, or after `/reload-plugins` in a session. The checkout must hold the plugin: `plugins/claude-code` on the branch you have checked out.

The plugin asks for no key and no server. Section 3 and section 4 provide both.

## 6. Check the install

Run these from the project folder. Start a new session with `claude` first.

1. In a shell, `claude plugin list` lists `cairnbook@cairnbook` with its status.
2. In the session, type `/` and look for `/cairnbook:status`, which the live check's session listed. Run it. It runs `cairnbook status` through the plugin's launcher and shows the output: the project, the server and the active task, or `task none active`.
3. The session's start-up record names the plugin's server `plugin:cairnbook:cairnbook`, and in the live check it was connected. Its 15 tools are named `mcp__plugin_cairnbook_cairnbook__<tool>`. The live check did not capture `/mcp` output, so this guide does not say what `/mcp` prints. Section 7a is the check that counts.
4. The SessionStart hook adds a block to Claude's context at the start of each session. With no active task the block is one line: `Cairnbook: no task is active in <project>.` With a task it is five lines, and the last one is `The block above is the task to continue.` You can print the same text yourself from the project folder with `cairnbook hook session-start`.

The live check's log records the block as the output of the SessionStart hook, with exit code 0. Whether Claude Code shows the block on screen was not checked (issue #26). The Claude Code hooks reference says the output reaches Claude's context; the live check did not test that.

## 7. Record a task

### The tool names

The plugin's skill teaches Claude the Cairnbook task loop with these tools, under their short names: `recall`, `task_start`, `step_add`, `step_update`, `event_log`, `learning_add`, `task_close`, and `task_current` when the session-start block is missing. [MCP-Tools](MCP-Tools) lists all 15. In the live check's session each one was named `mcp__plugin_cairnbook_cairnbook__<short name>`, so `task_start` appeared as `mcp__plugin_cairnbook_cairnbook__task_start`. The skill and this guide use the short names.

### 7a. Record a task in a session (the check that counts)

This check runs the plugin from inside Claude Code and reads the result from a terminal. It is the check the install must pass. Run it in the project folder, after section 6.

Before the session, run two checks in the project folder. They make sure the session uses the plugin's server and no other.

1. Run `cairnbook status`. It should print `task        none active`, with eight spaces between `task` and `none`. If it names an active task, close that task first. Section 9 covers a refused `task start`, and the close it describes applies here.
2. Run `claude mcp get cairnbook`. If a manual server named exactly `cairnbook` is there, remove it with `claude mcp remove cairnbook -s <scope>` at each scope it shows, as section 8 describes. If `claude mcp get cairnbook` shows no scope, use the fallback in section 8 instead. Leave the plugin's own server, `plugin:cairnbook:cairnbook`, alone: it is not the entry named `cairnbook`. If there is no manual `cairnbook` entry, there is nothing to remove.
3. Start Claude Code in the project folder with `claude`.
4. Ask Claude, in your own words, to record one small piece of work as a Cairnbook task. Give your agent name, so Claude passes it on every write, and tell Claude to use the Cairnbook MCP tools, not the `cairnbook` command in the shell. For example:

   ```text
   Start a Cairnbook task for one small piece of work you can finish in this session. Use the Cairnbook MCP tools, not the cairnbook command in the shell. Use <your-agent-name> as the agent on every Cairnbook write. Add one step, start it, do the work, finish it with a summary, and close the task with a one-line outcome.
   ```

   Claude uses the plugin's tools for this: `task_start`, `step_add`, `step_update` to start and finish the step, and `task_close`. A step must be started before it can be finished, and Claude does that. Claude Code may ask permission the first time each Cairnbook tool is used. Allow them.

5. Watch the tool calls Claude makes. A loop action is one of the four calls that record the task: `task_start`, `step_add`, `step_update` and `task_close`. Claude Code shows each call's name. The check counts only when these four calls are named as shown:

   - `mcp__plugin_cairnbook_cairnbook__task_start`
   - `mcp__plugin_cairnbook_cairnbook__step_add`
   - `mcp__plugin_cairnbook_cairnbook__step_update`
   - `mcp__plugin_cairnbook_cairnbook__task_close`

   If a loop action is done with a `mcp__cairnbook__…` call, or with a Bash `cairnbook …` command, in place of the plugin's call, the check did not exercise the plugin. Fix the cause (for a `mcp__cairnbook__` call, that is a manual entry, as step 2 covers), then run the check again from step 1 in a new session. Other calls to the plugin's server, such as `task_current`, do not fail the check.

   A refused first `task_close` is expected when Claude leaves out something the close requires: a summary, criteria results, lessons or a reason for none, or mistakes. The refusal's `missing` list names what is short. The skill tells Claude to supply each item and close again. A refusal is not an install failure.

6. When Claude has closed the task, run these in a terminal in the project folder:

   ```bash
   cairnbook task list
   cairnbook task get <task-id> --section steps
   ```

   Replace `<task-id>` with the first word on the first line that `task list` prints.

The check passes when the four calls in step 5 are named as shown, no loop action went through a `mcp__cairnbook__` call or a Bash `cairnbook` command in place of the plugin's call, and both of these hold:

- `cairnbook task list` shows the task with status `done`, one line, for example:

  ```text
  task-20261117-01  done  normal  Add one note to the scratch file
  ```

- `cairnbook task get <task-id> --section steps` lists the step as `[done]`, with the summary Claude wrote.

A Bash `cairnbook` command that is not a loop action, such as an incidental `cairnbook status`, does not fail the check.

If the task is missing, the session and the terminal are on different projects or servers. Run `cairnbook status` in the same folder and compare its project and server lines with the `.cairnbook.toml` file.

### 7b. Walk through one task in a terminal (checks the server and the CLI, not the plugin)

This walk-through runs the server and the CLI from a shell, with the CLI verbs in [CLI-Reference](CLI-Reference). It does not start Claude Code, so it checks the server and the CLI, not the plugin. It is useful for a shell-only agent, and for a quick check of the server, but it does not count for the install check in 7a. Run it in the project folder:

```bash
cd <project>
export CAIRNBOOK_AGENT=<your-agent-name>
cairnbook recall --goal "Record one task from the install guide" --agent <your-agent-name>
cairnbook task start --title "Try Cairnbook" --goal "Record one task from the install guide." --json
```

The JSON that `task start` prints has an `id` field. Copy its value and set it once:

```bash
export TASK=<task-id>
cairnbook step add "$TASK" --title "Write the first note"
cairnbook step update "$TASK" 1 --action start
cairnbook step update "$TASK" 1 --action finish --summary "Written"
cairnbook event log "$TASK" --kind note --text "Recorded from the install guide"
echo '{"lessons":[],"mistakes":[]}' | cairnbook task close "$TASK" --outcome done --summary "Install guide walked through" --no-lessons-reason "Nothing new" --json-input
cairnbook status
```

The last line prints `task        none active` (eight spaces between `task` and `none`), because the task is closed. The first two lines name the project and the server. `cairnbook task list` shows the task among the project's tasks.

On the CLI, `CAIRNBOOK_AGENT` names you as the caller on every write that leaves `--agent` out, so these verbs need no `--agent`. This holds for the CLI only. The plugin's MCP writes store the `agent` argument that Claude passes, and do not read `CAIRNBOOK_AGENT`.

## 8. If you ran `cairnbook enable` first

The plugin and `cairnbook enable --agent claude-code` are alternatives (D34). The plugin carries the same three hooks, so keep one route.

- **Duplicate hooks.** `cairnbook doctor --agent claude-code` warns when the plugin is enabled and cairnbook hooks are also in a settings file. Its `plugin` line names each file that holds one: the project's `.claude/settings.local.json` or `.claude/settings.json`, or the user's `~/.claude/settings.json`. Without the warning, each hook runs twice.
- **Removing enable's hooks.** `cairnbook enable` has no flag to remove them. Edit the files that `doctor` names by hand. Under `hooks`, delete the entries whose command is `cairnbook hook session-start`, `cairnbook hook pre-compact` or `cairnbook hook stop`, and keep every other entry. Before `enable` changed a file, it copied the original to the cache folder it names when it ran. Keep that copy if you want the file back.
- **A manual MCP entry.** If you also added cairnbook with `claude mcp add`, the session shows two servers: the plugin's `plugin:cairnbook:cairnbook` and your `cairnbook`. In the live check both were connected, each with 15 tools: the plugin's as `mcp__plugin_cairnbook_cairnbook__<tool>`, the manual one as `mcp__cairnbook__<tool>`. The two do not merge. Once the plugin is installed, remove the manual entry. Run `claude mcp get cairnbook`, note the scope it shows, and remove that entry with `claude mcp remove cairnbook -s <scope>`, for example `claude mcp remove cairnbook -s user`. Then run `claude mcp get cairnbook` again. Repeat until it finds no `cairnbook` entry. If `claude mcp remove` reports an error, read the scope it names and use that. If `claude mcp get cairnbook` shows no scope, the fallback is `claude mcp remove cairnbook` with no `-s`. Not confirmed: that fallback's behaviour. It comes from the `claude mcp remove --help` text, which says that without `-s` the command removes the entry from whichever scope holds it. This guide has read that text and has not run the fallback, so it is not live-checked. Also not live-checked: how `claude mcp get` reports an entry held at more than one scope. The plugin brings its own MCP server.
- **An old `cairnbook` on PATH.** The plugin's launcher runs the first `cairnbook` it finds on PATH, and only then the one in `~/.local/bin`. A `cairnbook` that comes earlier on PATH can be an older build than the checkout the plugin comes from. When you update the checkout, rebuild and reinstall the binary: `make build`, then `install -m 0755 bin/cairnbook ~/.local/bin/cairnbook`. `command -v cairnbook` shows the file the launcher will run first.

After the edits, `cairnbook doctor --agent claude-code` should show no `plugin` warning.

## 9. Troubleshooting

**Exit 127, `cairnbook` not found.** The plugin's launcher, `cairnbook-launch`, looks for `cairnbook` on PATH and then in `~/.local/bin`. When it finds neither, it prints one line on stderr and exits 127:

```text
cairnbook-launch: cairnbook is not on PATH or in ~/.local/bin. Install it: in your cairnbook checkout, run make build, then mkdir -p ~/.local/bin && install -m 0755 bin/cairnbook ~/.local/bin/cairnbook (docs/QUICK-INSTALL.md, section 1)
```

Run section 2, then check with `command -v cairnbook`. The live check did not reproduce this case, because `cairnbook` was installed on its machine. How Claude Code shows a server or hook that exits 127 is not confirmed (issue #26).

**`no project: run cairnbook init in the project, or set CAIRNBOOK_PROJECT`.** The folder has no `.cairnbook.toml` above it. Run section 4 in the project folder, or start Claude Code from that folder.

**A `doctor` line says `fail`.** The `project` line means no `.cairnbook.toml`. The `server` line means no server URL. The `key` line means no key in `CAIRNBOOK_API_KEY` or `client.env`. The `reach` line means the server does not answer as Cairnbook, or refuses the key. Each fix is in sections 3 and 4.

**No Cairnbook block at session start.** The skill's rule is that a missing block means the hook did not run or could not read the task. Run `cairnbook hook session-start` in the project folder. It prints the block, or the one-line no-task message. If it prints nothing, it says why on stderr: fix that cause, then run `cairnbook doctor`.

**`task start` is refused.** A task is already active. `cairnbook status` names it. Close it, then start the new one. The close needs its input: a summary, lessons or a reason for having none, and mistakes. That is why the short form, `cairnbook task close <task-id> --outcome done`, is refused. Use the full close from section 7b instead. Its `--json-input` with `{"lessons":[],"mistakes":[]}` on standard input, together with `--no-lessons-reason`, is what satisfies it. [CLI-Reference](CLI-Reference) gives the close flags.

## Where to read next

- [Quick-Start](Quick-Start): one machine, the whole path, with a "you should see" line for each step.
- [Install-Local](Install-Local): the same install in detail, with the files and environment variables.
- [Connecting-Agents](Connecting-Agents): the route without the plugin, and the other clients.
- [CLI-Reference](CLI-Reference) and [MCP-Tools](MCP-Tools): every verb and every tool.
