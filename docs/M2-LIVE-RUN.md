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

Do this on the server, in this order. [DEPLOY.md](DEPLOY.md) has the reasons. The key is
made **before** the server starts, for every variant below: a server on a non-loopback
address refuses to start with no active key, and `key add` needs the state directory
to exist first (it creates the database file, not its directory).

- [ ] **1.1 Build or install `cairn`.** From a checkout: `make build`, which writes
  `bin/cairn`. Install it: `install -m 0755 bin/cairn /usr/local/bin/cairn`.
- [ ] **1.2 Prepare the host and make an agent key.** This is the key Claude Code will
  use. Choose how the server will run, then do the matching block.

  *Run as the unit (setups a and b, the tunnel route).* As root, create the user and the
  state directory, then make the key as that user:

  ```bash
  useradd --system --no-create-home --home-dir /var/lib/cairn --shell /usr/sbin/nologin cairn
  install -d -m 0700 -o cairn -g cairn /var/lib/cairn
  sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name m2-live-run --scope agent
  ```

  If the user already exists, `useradd` says so and you can go on.

  *Run in a terminal for this run.* Use a dedicated database file that the run owns. The
  current directory exists, so no directory step is needed:

  ```bash
  cairn key add --db ./m2-live-run.db --name m2-live-run --scope agent
  ```

  The key is printed once, on stdout. Copy it now. It is the first key on this server, so
  DEPLOY.md's `ops` key is not needed for this run.
- [ ] **1.3 Start the server, one of five ways.** Do exactly one.
  - *TLS by files (setup a, unit):* DEPLOY.md, "Setup (a)", steps 4 to 6: install the
    certificate files, install the unit and the override, start it. The user, the state
    directory and the key from 1.2 are what make step 6 start.
  - *Behind a proxy on this host (setup b, unit):* DEPLOY.md, "Setup (b)", steps 3 and
    5: install the unit and start it, then configure the proxy. The server is on
    loopback, so it would also start with no key, but 1.2 made one.
  - *Behind a proxy on another host (setup b, unit):* DEPLOY.md, "Setup (b)", the
    paragraph "If the proxy is **not on this host**": install the unit and the
    `--behind-proxy` override, start it, then configure the proxy. This needs the key
    from 1.2 to start.
  - *No public DNS name (SSH tunnel, unit):* the server stays on loopback. DEPLOY.md,
    "Setup (b)", step 3 (install the unit and start it), and no proxy. The client opens
    the tunnel in 2.3.
  - *In a terminal for this run:* keep this terminal for the server, and capture its log:

    ```bash
    cairn serve --db ./m2-live-run.db 2>&1 | tee serve.log
    ```

    This listens on loopback, so it needs the tunnel route to be reached from the
    client. To serve TLS from a terminal, add `--addr 0.0.0.0:8788 --tls-cert <cert.pem>
    --tls-key <key.pem>`. Run every later server-side command in a **second** terminal.

  **For a unit, check it as it starts.** The unit has not been run under systemd before
  this run, so these three checks are part of the result. Run them and keep the output:

  ```bash
  systemd-analyze verify /etc/systemd/system/cairn.service
  systemctl status cairn
  systemd-analyze security cairn
  ```

  `verify` prints nothing when it finds no fault. `status` should show `active
  (running)`; paste its first lines. `security` prints a table and a final overall
  exposure line; paste the whole output. If the unit does not start, paste `status` and
  `journalctl -u cairn -n 30 --no-pager` (as root), and stop: that is a finding, not
  something to work round.
- [ ] **1.4 Check the server answers.** `curl -sS <server-url>/healthz` should print a
  fixed body. For setup (a) use `https://` and the port; for setup (b) use the proxy's
  address. For a tunnel, open it first (2.3) and use `http://127.0.0.1:8788/healthz`
  on the client. The health check needs no key.

## Part 2: the client

Do this on the second machine, in the project's directory.

- [ ] **2.1 Install `cairn` on the client.** The client needs: Claude Code, a test
  project directory, `curl` for the health check, a network route to the server (or
  `ssh` for the tunnel route), and a `cairn` binary built for the client's operating
  system and CPU. A binary built on Linux does not run on macOS. Two ways to get one:

  *Build on the build machine, for the client.* From the checkout, with Go 1.26 or later
  (the `go` line in `go.mod`), pick the client's target. On macOS, `uname -m` prints
  `arm64` (Apple silicon) or `x86_64` (Intel, which is `amd64` here):

  ```bash
  GOOS=darwin GOARCH=arm64 go build -o cairn ./cmd/cairn   # Apple silicon
  GOOS=darwin GOARCH=amd64 go build -o cairn ./cmd/cairn   # Intel Mac
  GOOS=linux  GOARCH=amd64 go build -o cairn ./cmd/cairn   # Linux client, x86_64
  ```

  Copy the file to the client, for example `scp cairn client:`. A binary built this way
  reports its version as `dev`.

  *Or build on the client,* from a checkout there, which needs Go 1.26 or later and
  `make`: `make build`, which writes `bin/cairn`.

  Then put it on the client's `PATH`. `~/.local/bin` does not exist on a stock macOS, so
  make it first:

  ```bash
  mkdir -p ~/.local/bin
  install -m 0755 cairn ~/.local/bin/cairn      # or bin/cairn, if you ran make build
  ```

  Add it to `PATH` if it is not there. For zsh (the macOS default), then open a new
  terminal, or run the `export` line in the current one:

  ```bash
  echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
  export PATH="$HOME/.local/bin:$PATH"
  ```

  For bash, use `~/.bashrc` on Linux, or `~/.bash_profile` on macOS, in place of
  `~/.zshrc`. Check it: `cairn version`.
- [ ] **2.2 Write the key file, owner-only.** The key goes into
  `~/.config/cairn/client.env`, mode 0600. This form works in bash and zsh, and keeps the
  key out of your shell history:

  ```bash
  mkdir -p -m 700 ~/.config/cairn && chmod 700 ~/.config/cairn
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

  Use `https://` and the server's name for setup (a) and for setup (b). `cairn doctor`
  warns when a key would go over plain `http://` to a host that is not loopback.

  **For a server with no public DNS name, use the tunnel.** Open it on the client and
  leave it running for the whole run, in its own terminal:

  ```bash
  ssh -N -o ExitOnForwardFailure=yes -L 8788:127.0.0.1:8788 you@server
  ```

  Then set `server = "http://127.0.0.1:8788"`. The client sees a loopback address, so
  `cairn doctor` does not warn, and the SSH tunnel carries the traffic encrypted.
  `cairn` trusts only the operating system's certificate roots and has no flag to skip
  a check; DEPLOY.md, "A server with no public DNS name", also covers a private CA.
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
  only the lines that have `msg=listening`, and any with `level=ERROR`. Redact the host
  name or the address if you wish, by replacing it with `<server>`. No key should appear
  in these lines. If one does, say so in your reply and do not paste the line.

  - *For the unit:* `journalctl` needs root, or membership of the `systemd-journal`
    group. Run it as root or with `sudo`:

    ```bash
    sudo journalctl -u cairn --no-pager | grep -E 'msg=listening|level=ERROR'
    ```

  - *For a server run in a terminal:* it has no journal entry. Its log is the
    `serve.log` that 1.3 captured with `tee`, so use that file:

    ```bash
    grep -E 'msg=listening|level=ERROR' serve.log
    ```

    If you did not capture it, copy the lines from that terminal's scrollback.

## Paste this back to Bishop

Paste these, in this order, in one message:

1. The output of `cairn doctor` (from 3.1, or the block from 5.2).
2. The output of `cairn export task <task-id>` (from 5.1, or the block from 5.2).
3. The serve log lines from 5.4, redacted.
4. The output of `cairn key list` from 5.3.
5. For a unit only: the output of the three checks from 1.3 (`systemd-analyze verify`,
   `systemctl status cairn`, `systemd-analyze security cairn`).

Do not paste the key itself, or the contents of `client.env`. The key should appear in
none of them; if it does, say so and leave that line out.

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
