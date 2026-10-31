# M2 live run: checklist

This is the live run that closes M2's exit check (issue #19). A Claude Code session on a
second machine starts, steps and closes a small task through `cairn mcp`, against a
`cairn serve` on the server. It takes about 20 minutes. The operator runs it and pastes
the output back. Nothing here needs the author to be present.

Two machines are involved:

- **Server:** the machine that runs `cairn serve`. Setup is in [DEPLOY.md](DEPLOY.md).
- **Client:** the machine with Claude Code and a project. It is the second machine.

Keep one test project for the run. Its name below is `m2-live-run`.

## Part 1: the server

Do this on the server. [DEPLOY.md](DEPLOY.md) has the reasons.

- [ ] **1.1 Build or install `cairn`.** From a checkout: `make build`, which writes
  `bin/cairn`. Install it: `install -m 0755 bin/cairn /usr/local/bin/cairn`.
- [ ] **1.2 Start the server, one of two ways.**
  - *TLS by files (setup a):* `cairn serve` on a non-loopback address with
    `--tls-cert` and `--tls-key`. DEPLOY.md, "Setup (a)".
  - *Behind a proxy (setup b):* `cairn serve` on loopback, which is the default, behind
    Caddy or nginx with TLS. DEPLOY.md, "Setup (b)".

  Start it with the unit from DEPLOY.md, or in a terminal for this run. For a terminal,
  use a dedicated database file that the run owns, for example
  `cairn serve --db ./m2-live-run.db`.
- [ ] **1.3 Make an agent key.** This is the key Claude Code will use:

  ```bash
  cairn key add --db ./m2-live-run.db --name m2-live-run --scope agent
  ```

  The key is printed once, on stdout. Copy it now. Use the same `--db` as the server.
  If you run the unit, run `key add` as `sudo -u cairn` against
  `/var/lib/cairn/cairn.db`, as DEPLOY.md says.
- [ ] **1.4 Check the server answers.** `curl -sS <server-url>/healthz` should print a
  fixed body. For setup (a) use `https://` and the port; for setup (b) use the proxy's
  address. The health check needs no key.

## Part 2: the client

Do this on the second machine, in the project's directory.

- [ ] **2.1 Install `cairn`.** Build it from a checkout as in 1.1, then copy it to a
  directory on your `PATH`, for example `install -m 0755 bin/cairn ~/.local/bin/cairn`.
  Check it: `cairn version`.
- [ ] **2.2 Write the key file, owner-only.** The key goes into
  `~/.config/cairn/client.env`, mode 0600. This form works in bash and zsh, and keeps the
  key out of your shell history:

  ```bash
  mkdir -p -m 700 ~/.config/cairn
  [ -e ~/.config/cairn/client.env ] && chmod 600 ~/.config/cairn/client.env
  printf 'API key: ' >&2; read -rs K; echo >&2
  (umask 077 && printf 'CAIRN_API_KEY=%s\n' "$K" > ~/.config/cairn/client.env); unset K
  ```

  Paste the key at the prompt. Check the mode: `ls -l ~/.config/cairn/client.env` must
  begin `-rw-------`. A file that any other user can read is refused, and `cairn` says
  `chmod 600`.
- [ ] **2.3 Write `.cairn.toml` in the project root.** It holds the project name and the
  server URL, and no key. Write it by hand, or run
  `cairn init --project m2-live-run --server <server-url>` from the project directory.

  ```toml
  project = "m2-live-run"
  server = "https://cairn.example.net"
  ```

  Use `https://` for setup (a) and for setup (b). `cairn doctor` warns when a key would
  go over plain `http://` to a host that is not loopback.
- [ ] **2.4 Write `.mcp.json` by hand, in the project root.** It registers `cairn mcp`.
  Use the absolute path of the binary, since Claude Code may not have your `PATH`:

  ```json
  {
    "mcpServers": {
      "cairn": {
        "command": "/Users/you/.local/bin/cairn",
        "args": ["mcp"]
      }
    }
  }
  ```

  On Linux the path is, for example, `/home/you/.local/bin/cairn`. Use what
  `command -v cairn` prints.

## Part 3: `cairn doctor` passes

- [ ] **3.1 Run it from the project directory:** `cairn doctor`.

  It checks the binary, the project file, the server, the key and the connection. It
  changes nothing. Every check should say `ok`. A `warn` is printed with its reason; a
  `fail` means stop and fix it before going on. The exit code must be 0.

## Part 4: the Claude Code session

- [ ] **4.1 Open Claude Code in the project.** Approve the project's `cairn` MCP server
  when Claude Code asks. Then run `/mcp` and check that `cairn` is connected.
- [ ] **4.2 Ask for a small task, through the MCP tools only.** Paste this into the
  session:

  > Using the cairn MCP tools, not the shell: start a task titled "M2 live run" with a
  > one-line goal, using `task_start` with `agent` set to `claude-code`. Then set two
  > steps with `step_plan`: "write a line", and "read it back". Start and finish step 1
  > with `step_update`, and record a short `event_log` entry for it. Then close the task
  > with `task_close`, outcome done, with a one-line summary. If the close is refused, it
  > lists what is missing: supply it and close again. Tell me the task id.

- [ ] **4.3 Note the task id.** Claude Code reports it, in the form `task-YYYYMMDD-NN`.

## Part 5: export and report

- [ ] **5.1 Export the task:** `cairn export task <task-id>`. It prints the task as
  Markdown, with its steps and events. It should show the title, the two steps and the
  close.
- [ ] **5.2 Run the check script** from the project directory, with the task id:

  ```bash
  scripts/m2-live-run-check.sh <task-id>
  ```

  It runs `cairn doctor` and `cairn export task`, and prints both in one block. It never
  prints the key, and it creates and changes nothing. If `cairn` is not on your `PATH`,
  run it as `CAIRN=/full/path/to/cairn scripts/m2-live-run-check.sh <task-id>`.
- [ ] **5.3 On the server, list the keys:**

  ```bash
  cairn key list --db ./m2-live-run.db
  ```

  Or, for the unit, `sudo -u cairn /usr/local/bin/cairn key list --db /var/lib/cairn/cairn.db`.
  The list shows names, scopes and times. It never shows a key.
- [ ] **5.4 Pick the serve log lines.** Take the server's log lines for the run, and keep
  only the lines that have `msg=listening`, and any with `level=ERROR`. For the unit:

  ```bash
  journalctl -u cairn --no-pager | grep -E 'msg=listening|level=ERROR'
  ```

  Redact the host name or the address if you wish, by replacing it with `<server>`. No
  key should appear in these lines. If one does, say so in your reply and do not paste
  the line.

## Paste this back to Bishop

Paste four things, in this order, in one message:

1. The output of `cairn doctor` (from 3.1, or the block from 5.2).
2. The output of `cairn export task <task-id>` (from 5.1, or the block from 5.2).
3. The serve log lines from 5.4, redacted.
4. The output of `cairn key list` from 5.3.

Do not paste the key itself, or the contents of `client.env`. The key should appear in
none of the four; if it does, say so and leave that line out.

## Part 6: clean up

- [ ] **6.1 Revoke the test key** on the server, once the run is pasted back:

  ```bash
  cairn key revoke --db ./m2-live-run.db --name m2-live-run
  ```

  For the unit, run it as `sudo -u cairn` against `/var/lib/cairn/cairn.db`. A revoked
  key stops working at its next call, and its name is not reused.
- [ ] **6.2 Remove the client's key file** if the machine is not kept for further runs:
  `rm ~/.config/cairn/client.env`. Remove `.mcp.json` from the test project.
- [ ] **6.3 Stop a terminal server** if you started one for this run. Stop the unit only
  if it was started for this run.

The test task stays in the database. The server has no call to delete a task, so it is
left as history. Closing the run does not close issue #19: the operator does that, when
the output has been checked.
