# M4 hand check: hooks, `/compact` and the clients

Status as of 2026-11-05: not yet run. M4's exit check is that, on Claude Code, a task
started before `/compact` continues after it with no user prompt, and that an agent
with only a shell completes and closes a task through the CLI. The automated tests
cover the CLI, the hooks and `enable`. The automated exit test covers the shell-only
close. No automated test runs `/compact` on Claude Code, so section 3 is the check for
that part. Until it is recorded, the status lines in the README, PLAN and the wiki stay
at "M4 is current", as [PLAN §9](PLAN.md#9-project-management) requires.

This guide follows the shape of [M3-UI-CHECK.md](M3-UI-CHECK.md): numbered steps, exact
commands, what to look for, and a list of what to paste back. It takes about forty
minutes. The operator runs it and pastes the results back.

Placeholders are in angle brackets. Type the value without the brackets. This guide
uses no real host names, keys or local paths:

| Placeholder | What it stands for |
|-------------|--------------------|
| `<server-url>` | The `cairn serve` address, such as `https://<server>:8788`, or `http://127.0.0.1:8788` for a loopback server |
| `<project>` | The test project's name, which you choose, such as `m4-hand-check` |
| `<project-dir>` | The scratch directory the test project lives in, outside any repository you care about |
| `<log-file>` | A log file path outside the project, for the Claude Code debug log |
| `<agent-name>` | The calling role, such as `hand-check` |
| `<task-id>` | The id `cairn task start` prints |
| `<key>` | The agent key. Set it in the environment, and never type it into a command or paste it back |

## 1. Purpose and scope

The hooks are the part of M4 that only a live client can settle. The Claude Code
reference says what each hook's output does, and section 3 checks that the agent
behaves that way. Sections 4 and 5 are the EXPERIMENTAL clients. They check paths that
no real run has confirmed yet.

The check changes only the scratch project and its files. It writes to the server's
database through the normal CLI and MCP calls, so a test task and test steps are left
on the server. Section 7 cleans up what the check made.

## 2. Before you start

- [ ] **2.1 Build `cairn` and put it on the PATH the client uses.** The hooks run
  `cairn` by name, so Claude Code must find it on its own PATH, not only in your
  shell. From the checkout, run `make build`, then install `bin/cairn` to a directory
  on that PATH. Check it with `cairn version`.
- [ ] **2.2 Make a scratch project directory.** Create `<project-dir>` with `mkdir`, and
  do not use a directory that holds other work.
- [ ] **2.3 Set the server and the key in the environment.** Export `CAIRN_API_KEY` with
  an agent-scoped key that the operator made for this check (`cairn key add`, on the
  server). Do not put the key in a file in the scratch directory.
- [ ] **2.4 Initialise the project.** From `<project-dir>`:

  ```bash
  cairn init --project <project> --server <server-url>
  ```

  It writes `.cairn.toml` and no key. Check the file holds only `project` and `server`.
- [ ] **2.5 Check the server answers.** Run `cairn doctor` from `<project-dir>`. It
  should report the project, the server and a Cairn server, each `ok`.

## 3. Claude Code: the `/compact` exit check

Run this section in a Claude Code session started in `<project-dir>`. Start Claude Code
with a debug log file, so the hook entries can be read afterwards:

```bash
claude --debug-file <log-file>
```

The hooks reference at <https://code.claude.com/docs/en/hooks.md> is the source for
what Claude Code does with each hook's output. This check is what settles it in
practice. The reference does not run a session for you.

- [ ] **3.1 Preview the hooks.** From `<project-dir>`, run:

  ```bash
  cairn enable --agent claude-code --dry-run
  ```

  It should list three hooks: `SessionStart` with the `compact` matcher, `PreCompact`
  and `Stop`. Nothing is written by a dry run.
- [ ] **3.2 Install the hooks.** Run the same command without `--dry-run`. It writes
  `.claude/settings.local.json` in `<project-dir>`. Check that the file holds the three
  cairn hooks and nothing else you did not expect. It writes no MCP registration for
  Claude Code. Register the server by hand for this check, as section 3.3 says, or
  through the M5 plugin when that exists.
- [ ] **3.3 Register the MCP server, or use the CLI only.** For this check the agent
  may use either the CLI or the MCP server. To use MCP, register it in Claude Code
  from `<project-dir>`:

  ```bash
  claude mcp add cairn -- cairn mcp
  ```

  This is the local scope, which is the default, so it stays on this machine. The
  server reads `CAIRN_API_KEY` from the environment Claude Code starts in. Then check
  that the tools `task_current` and `task_start` are listed. The CLI needs nothing
  more than section 2. With either route, a task read the agent makes on its own after
  `/compact` makes the attempt not run, as the evidence note under What to look for says.
- [ ] **3.4 Run `cairn doctor --agent claude-code`.** Record its output. Expect `ok`
  for `SessionStart` and `Stop`. A `PreCompact` line that is `ok` is expected, since
  the hook is installed as D15 names it. A `SessionStart` warning that the matcher does
  not name `compact` is a fail of this step: fix the matcher in
  `.claude/settings.local.json` by hand, then run the doctor again.
- [ ] **3.5 Start a task with two steps, from a separate terminal.** Open a second
  terminal, outside the Claude Code session, and run these commands there from
  `<project-dir>`, with `CAIRN_API_KEY` set as section 2.3 says:

  ```bash
  cairn task start --title "hand check" --goal "Check the /compact carry" --agent <agent-name>
  cairn step plan <task-id> --json-input --agent <agent-name>
  ```

  For the second command, put
  `{"steps": [{"title": "first", "agent": "<agent-name>"}, {"title": "second", "agent": "<agent-name>"}]}`
  on standard input. Do not start the task or set its next action through the Claude Code session or its
  MCP tools. Tell the operator not to mention the task in the session before `/compact`.
  The task's facts must reach the agent only through the hook.
- [ ] **3.6 Start step 1, and set a marker as the next action.** In the same separate
  terminal, run `cairn step update <task-id> 1 --action start --agent <agent-name>`.
  Then set the next action to a marker that nothing else in the project uses:

  ```bash
  cairn task update <task-id> --next-action "<distinctive-next-action>" --agent <agent-name>
  ```

  Replace `<distinctive-next-action>` with a unique phrase you choose, of two or three
  words that appear nowhere else in this guide or the project. Write the phrase down.
  It is the marker the pass under What to look for depends on. Check it with
  `cairn task current` before you leave the terminal.
- [ ] **3.7 Run `/compact` in the Claude Code session.** Type `/compact` and let it
  finish. Do not type a message after it yet. Note the time of `/compact` in UTC.
- [ ] **3.8 Watch whether a turn starts, with no prompt from you.** Wait for the
  session to show a reply after `/compact` has finished. This guide does not know
  whether Claude Code starts a turn by itself after a manual `/compact`. The check is
  what records it.
  - If a reply appears on its own, read it, and go on to the pass and fail lines below.
  - If no reply appears, send one fixed, neutral message in the session: `continue`.
    Send exactly that text and nothing else. Record that you sent it, its exact text
    and the time in UTC, then read the reply. Sending it does not fail the check. It
    still counts as a pass if the agent carries on with the task.

What to look for:

- **Pass.** The agent's first reply after `/compact` names the marker from 3.6, or acts
  on it: it works on step 1 as the marker says. The check does not depend on whether a
  turn started by itself or after the neutral message from 3.8, and sending that message
  does not fail the check.
- **Fail.** The reply does not name or act on the marker. Or the agent asks what the
  task is, starts unrelated work, or takes no step from the task. Or the doctor warned
  about the matcher in 3.4 and the matcher was not fixed.
- **Not run.** The agent made a task read of its own after the compact, as the note
  below describes. Start again from 3.5 with a new task and a new marker.

A note on evidence. The marker is the evidence. It was set outside the session, so the
conversation before `/compact` cannot hold it, and the compaction summary cannot carry
it. The only other source is a task read the agent makes on its own. If the agent calls
`task_current` or `cairn task current` after the compact, record the call. The reply
then does not show that the hook carried the task, so the attempt is not run. The debug
log line in 3.9 is supporting evidence only. It shows that the hook ran, not that the
agent used its output, so it does not decide the verdict.

- [ ] **3.9 Read the debug log for the hook runs.** Search `<log-file>` for the
  `SessionStart` hook entries, and find the entry after the time of `/compact`. Record
  the entry's time and its event name. Redact any path in it, as section 6 says. This
  is supporting evidence, and it does not decide the verdict.
- [ ] **3.10 (Optional) Check the Stop warning.** With step 1 still active and no
  summary, end a turn. The Stop hook is meant to show a warning to you: its
  `systemMessage` is a warning shown to the user, not context for the agent. Record
  whether the warning appeared and what it said. The message names the step.

## 4. OpenCode (EXPERIMENTAL, not yet run)

`cairn enable --agent opencode` writes `opencode.json` and `.cairn/instructions.md`
relative to the project root. [DESIGN.md §6.2](DESIGN.md#62-other-clients) and the
enable help say so, but no real OpenCode run has confirmed that base path. This section
is the check. Record what OpenCode actually does, whatever it is. A finding here is
useful, and it is not a failure of the check.

- [ ] **4.1 Enable OpenCode in the scratch project.** From `<project-dir>`, run
  `cairn enable --agent opencode --dry-run`, then the same command without it. Note the
  lines it prints, which say EXPERIMENTAL.
- [ ] **4.2 Start OpenCode from the project root.** Run it with `<project-dir>` as the
  working directory. Do not start it from a parent directory.
- [ ] **4.3 Confirm the instructions file is loaded from the root.** UNVERIFIED: how
  OpenCode reports the instruction files it loaded is not known to this guide. Use
  whatever OpenCode shows for loaded instructions, and record the path it reports. The
  expected result is `.cairn/instructions.md` under `<project-dir>`.
- [ ] **4.4 Confirm `opencode.json` is read from the root.** Check that OpenCode lists a
  `cairn` MCP server. UNVERIFIED: `enable` says the tools arrive with the prefix
  `cairn_`, and no OpenCode run has confirmed that. Record the names of the tools
  OpenCode shows, whatever they are.
- [ ] **4.5 Run `cairn doctor --agent opencode`.** Record its output. It changes
  nothing.

Record the OpenCode version. If the path in 4.3 or 4.4 is not the project root, stop
that section and record the path OpenCode used, instead of moving files to make it
work.

## 5. pi (EXPERIMENTAL, not yet run)

`cairn enable --agent pi` writes `.pi/mcp.json` and a cairn block in `AGENTS.md`. pi
reads `.pi/mcp.json` only after the project is trusted, and the enable output says so.
The tool prefix in the cairn block is `mcp__cairn__`, which is cairn's text and has not
been confirmed against a real pi run.

- [ ] **5.1 Enable pi in the scratch project.** From `<project-dir>`, run
  `cairn enable --agent pi --dry-run`, then the same command without it.
- [ ] **5.2 Start pi in the project and trust it.** When pi asks whether to trust the
  project, decline first, and check that the cairn tools are absent. That is the
  expected result, since `.pi/mcp.json` is not read yet. Then trust the project, and
  restart pi if it needs a restart.
- [ ] **5.3 Confirm the tools are present after trust.** Check that the cairn tools
  are listed, with the prefix `mcp__cairn__`, and that `exposure` `direct` lets the
  model see them. UNVERIFIED: the prefix and the exposure behaviour are cairn's
  settings and have not been checked in pi. Record what pi shows.
- [ ] **5.4 Run `cairn doctor --agent pi`.** Record its output. It changes nothing.

Record the pi version. The same rule as section 4 applies: a path or a prefix that
differs from the cairn text is a finding, to be recorded and not patched by hand.

## 6. What a hand checker records

Keep these in a note while you work, and paste the note back as section 8 says. Record
times in UTC, as you read them from your own clock. Do not estimate a time.

- The date and time, in UTC, when each step ran.
- The outputs of `cairn version`, `claude --version`, and the OpenCode and pi versions
  where you ran them.
- The operating system name and version. Not the host name.
- For each numbered step: done, skipped (with the reason), or failed (with what you saw).
- The `cairn doctor` output lines, verbatim, with every path replaced by its placeholder
  and the server address replaced by `<server-url>`. The doctor's server line carries the
  address the client was given, so that line needs the replacement.
- For section 3: the time of `/compact`; whether a turn started by itself and, if you sent
  the neutral message from 3.8, its exact text and the time; the marker from 3.6; the
  agent's first reply after the compact (up to three lines), and whether it names or acts
  on the marker; any task read the agent made on its own; and the debug log's
  `SessionStart` entry, with paths redacted.
- For sections 4 and 5: the path or prefix each client reported, and whether it matched
  the expected one, as a yes or a no.
- A verdict for sections 3, 4 and 5: pass, fail, or not run, with the reason.

Redact every path you paste. Replace the home directory, the project directory and the
log file path with the placeholders in the table above. Replace every server address,
including the one in the doctor's server line, with `<server-url>`.

## 7. Cleanup

- [ ] **7.1 Close the task.** Close the test task on the server with
  `cairn task close <task-id> --outcome done --summary "hand check" --agent <agent-name>`.
  A close may be refused for a missing lesson or a missing review. Give what it lists, or
  leave the task open and say so.
- [ ] **7.2 Remove the hooks from the scratch project.** Delete `<project-dir>` when you
  are done with it. A backup of a settings file that `enable` made is in the user's
  cache directory, under `cairn/backups/<project>`, outside the project. Delete the
  backup yourself. Nothing prunes it.
- [ ] **7.3 Revoke the test key.** Ask the operator to run `cairn key revoke` for the
  agent key you used. A key is not kept on the scratch project.

## 8. What to paste back

Paste these, in this order, in one message:

1. The `cairn version` output and the client versions from section 6.
2. The `cairn doctor --agent claude-code` output from 3.4, with the server address as
   `<server-url>` and the paths as their placeholders, as section 6 says, and the verdict
   for section 3.
3. The time of `/compact`, whether a turn started by itself or the neutral message from
   3.8 was sent (with its text and time), the marker from 3.6, and the agent's first reply
   after the compact, from 3.7 and 3.8, with any task read the agent made on its own.
4. The `SessionStart` debug log entry from 3.9, redacted.
5. The Stop warning from 3.10, if you ran it.
6. For sections 4 and 5: the paths and prefixes each client reported, the yes or no for
   each expected value, and the verdict.
7. The status of each step: done, skipped or failed, with the reason.

Never paste:

- a key of any kind, or the output of `cairn key add`;
- the contents of a client's settings file, beyond the lines naming a cairn hook;
- a host name, a server address or a full local path;
- an unredacted debug log.

If a key is pasted anywhere, revoke it and make a new one.
