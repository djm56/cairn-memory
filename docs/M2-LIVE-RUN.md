# M2 live run: checklist

This is the live run that closes M2's exit check (issue #19). A Claude Code session on a
second machine starts, steps and closes a small task through `cairnbook mcp`, against a
`cairnbook serve` on the server. It takes about 20 minutes. The operator runs it and pastes
the output back. Nothing here needs the author to be present.

Two machines are involved:

- **Server:** the machine that runs `cairnbook serve`. Setup is in [DEPLOY.md](DEPLOY.md).
- **Client:** the machine with Claude Code and a project. It is the second machine.

Keep one test project for the run. Its name below is `m2-live-run`.

## Part 1: the server

Do this on the server, in this order. [DEPLOY.md](DEPLOY.md) has the reasons. The key is
made **before** the server starts, for every variant below: a server on a non-loopback
address refuses to start with no active key, and `key add` needs the state directory
to exist first (it creates the database file, not its directory).

- [ ] **1.1 Build or install `cairnbook`.** From a checkout: `make build`, which writes
  `bin/cairnbook`. Install it: `install -m 0755 bin/cairnbook /usr/local/bin/cairnbook`.
- [ ] **1.2 Prepare the host and make an agent key.** This is the key Claude Code will
  use. Choose how the server will run, then do the matching block.

  *Run as the unit (setups a and b, the tunnel route).* As root, create the user and the
  state directory, then make the key as that user:

  ```bash
  useradd --system --no-create-home --home-dir /var/lib/cairnbook --shell /usr/sbin/nologin cairnbook
  install -d -m 0700 -o cairnbook -g cairnbook /var/lib/cairnbook
  sudo -u cairnbook /usr/local/bin/cairnbook key add --db /var/lib/cairnbook/cairnbook.db --name m2-live-run --scope agent
  ```

  If the user already exists, `useradd` says so and you can go on.

  *Run in a terminal for this run.* Use a dedicated database file that the run owns. The
  current directory exists, so no directory step is needed:

  ```bash
  cairnbook key add --db ./m2-live-run.db --name m2-live-run --scope agent
  ```

  The key is printed once, on stdout. Copy it now. It is the first key on this server, so
  DEPLOY.md's `ops` key is not needed for this run. Leave the `key add` output out of
  anything you paste back: it contains the key (see "Paste this back" below).
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
    cairnbook serve --db ./m2-live-run.db 2>&1 | tee serve.log
    ```

    This listens on loopback, so it needs the tunnel route to be reached from the
    client. To serve TLS from a terminal, add `--addr 0.0.0.0:8788 --tls-cert <cert.pem>
    --tls-key <key.pem>`. Run every later server-side command in a **second** terminal.

  **For a unit, check it as it starts.** The unit was first started under systemd in the
  2026-11-02 live run (see DEPLOY.md). The three checks stay part of every run's result.
  Run them and keep the output:

  ```bash
  systemd-analyze verify /etc/systemd/system/cairnbook.service
  systemctl status cairnbook
  systemd-analyze security cairnbook
  ```

  `verify` prints nothing when it finds no fault. `status` should show `active
  (running)`; paste its first lines. `security` prints a table and a final overall
  exposure line; paste the whole output. If the unit does not start, paste `status` and
  `journalctl -u cairnbook -n 30 --no-pager` (as root), and stop: that is a finding, not
  something to work round.
- [ ] **1.4 Check the server answers.** `curl -sS <server-url>/healthz` should print a
  fixed body. For setup (a) use `https://` and the port. For setup (b), check the proxy
  from the server without depending on its public address:
  `curl -sS --max-time 10 --resolve <name>:443:127.0.0.1 https://<name>/healthz`.
  This check applies only when the proxy runs on the same host as the server.
  From the client, setups (a) and (b) require one successful
  `curl -sS https://<name>/healthz`. A server-side `curl` of the public name may time
  out; that alone is not a failure. For a tunnel, open it first (2.3) and use
  `http://127.0.0.1:8788/healthz` on the client. The health check needs no key.

## Part 2: the client

Do this on the second machine, in the project's directory.

- [ ] **2.1 Install `cairnbook` on the client.** The client needs: Claude Code, a test
  project directory, `curl` for the health check, a network route to the server (or
  `ssh` for the tunnel route), and a `cairnbook` binary built for the client's operating
  system and CPU. A binary built on Linux does not run on macOS. Two ways to get one:

  *Build on the build machine, for the client.* From the checkout, with Go 1.26 or later
  (the `go` line in `go.mod`), pick the client's target. On macOS, `uname -m` prints
  `arm64` (Apple silicon) or `x86_64` (Intel, which is `amd64` here):

  ```bash
  GOOS=darwin GOARCH=arm64 go build -o cairnbook ./cmd/cairnbook   # Apple silicon
  GOOS=darwin GOARCH=amd64 go build -o cairnbook ./cmd/cairnbook   # Intel Mac
  GOOS=linux  GOARCH=amd64 go build -o cairnbook ./cmd/cairnbook   # Linux client, x86_64
  ```

  Copy the file to the client, for example `scp cairnbook client:`. A binary built this way
  reports its version as `dev`.

  *Or build on the client,* from a checkout there, which needs Go 1.26 or later and
  `make`: `make build`, which writes `bin/cairnbook`.

  Then put it on the client's `PATH`. `~/.local/bin` does not exist on a stock macOS, so
  make it first:

  ```bash
  mkdir -p ~/.local/bin
  install -m 0755 cairnbook ~/.local/bin/cairnbook      # or bin/cairnbook, if you ran make build
  ```

  Add it to `PATH` if it is not there. For zsh (the macOS default), then open a new
  terminal, or run the `export` line in the current one:

  ```bash
  echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
  export PATH="$HOME/.local/bin:$PATH"
  ```

  For bash, use `~/.bashrc` on Linux, or `~/.bash_profile` on macOS, in place of
  `~/.zshrc`. Check it: `cairnbook version`.
- [ ] **2.2 Write the key file, owner-only.** The key goes into
  `~/.config/cairnbook/client.env`, mode 0600. This form works in bash and zsh, and keeps the
  key out of your shell history:

  ```bash
  mkdir -p -m 700 ~/.config/cairnbook && chmod 700 ~/.config/cairnbook
  [ -e ~/.config/cairnbook/client.env ] && chmod 600 ~/.config/cairnbook/client.env
  printf 'API key: ' >&2; read -rs K; echo >&2
  (umask 077 && printf 'CAIRNBOOK_API_KEY=%s\n' "$K" > ~/.config/cairnbook/client.env); unset K
  ```

  Paste the key at the prompt. Check the mode: `ls -l ~/.config/cairnbook/client.env` must
  begin `-rw-------`. A file that any other user can read is refused, and `cairnbook` says
  `chmod 600`.
- [ ] **2.3 Write `.cairnbook.toml` in the project root.** It holds the project name and the
  server URL, and no key. Write it by hand, or run
  `cairnbook init --project m2-live-run --server <server-url>` from the project directory.

  ```toml
  project = "m2-live-run"
  server = "https://cairnbook.example.net"
  ```

  Use `https://` and the server's name for setup (a) and for setup (b). `cairnbook doctor`
  warns when a key would go over plain `http://` to a host that is not loopback.

  **For a server with no public DNS name, use the tunnel.** Open it on the client and
  leave it running for the whole run, in its own terminal:

  ```bash
  ssh -N -o ExitOnForwardFailure=yes -L 8788:127.0.0.1:8788 you@server
  ```

  Then set `server = "http://127.0.0.1:8788"`. The client sees a loopback address, so
  `cairnbook doctor` does not warn, and the SSH tunnel carries the traffic encrypted.
  `cairnbook` trusts only the operating system's certificate roots and has no flag to skip
  a check; DEPLOY.md, "A server with no public DNS name", also covers a private CA.
- [ ] **2.4 Write `.mcp.json` by hand, in the project root.** It registers `cairnbook mcp`.
  Use the absolute path of the binary, since Claude Code may not have your `PATH`.
  This is the documented route.

  ```json
  {
    "mcpServers": {
      "cairnbook": {
        "command": "/Users/you/.local/bin/cairnbook",
        "args": ["mcp"]
      }
    }
  }
  ```

  On Linux the path is, for example, `/home/you/.local/bin/cairnbook`. Use what
  `command -v cairnbook` prints.

  *User scope, as an alternative.* Run
  `claude mcp add --scope user cairnbook -- <absolute path> mcp`. It registers `cairnbook` for
  every project on this machine, and skips the approval prompt in
  4.1, so 4.1's approval check does not apply to it. Use one route or the other, not
  both. Its cleanup is in part 6.

## Part 3: `cairnbook doctor` passes

- [ ] **3.1 Run it from the project directory:** `cairnbook doctor`.

  It checks the binary, the project file, the server, the key and the connection. It
  changes nothing. Every check should say `ok`. A `warn` is printed with its reason; a
  `fail` means stop and fix it before going on. The exit code must be 0.

## Part 4: the Claude Code session

- [ ] **4.1 Open Claude Code in the project.** Approve the project's `cairnbook` MCP server
  when Claude Code asks. (Not for the user-scope route in 2.4, which asks for no
  approval.) Then run `/mcp` and check that `cairnbook` is connected.
- [ ] **4.2 Ask for a small task, through the MCP tools only.** Paste this into the
  session:

  > Using the cairnbook MCP tools, not the shell: start a task titled "M2 live run" with a
  > one-line goal, using `task_start` with `agent` set to `claude-code`. Then set two
  > steps with `step_plan`: "write a line", and "read it back". Start and finish each step
  > with `step_update`, and record a short `event_log` entry for each. Finish both steps
  > before you close. Then close the task with `task_close`, outcome done, with a one-line
  > summary. If the close is refused, it lists what is missing: supply it and close
  > again. Tell me the task id.

- [ ] **4.3 Note the task id.** Claude Code reports it, in the form `task-YYYYMMDD-NN`.

## Part 5: export and report

- [ ] **5.1 Export the task:** `cairnbook export task <task-id>`. It prints the task as
  Markdown, with its steps and events. It should show the title, the two steps and the
  close.
- [ ] **5.2 Run the check script** from the project directory, with the task id:

  ```bash
  scripts/m2-live-run-check.sh <task-id>
  ```

  It runs `cairnbook doctor` and `cairnbook export task`, and prints both in one block. It never
  prints the key, and it creates and changes nothing. If `cairnbook` is not on your `PATH`,
  run it as `CAIRNBOOK=/full/path/to/cairnbook scripts/m2-live-run-check.sh <task-id>`.
- [ ] **5.3 On the server, list the keys.** Use the block that matches the run from 1.2
  and 1.3. The list shows names, scopes and times. It never shows a key.

  *For the unit:*

  ```bash
  sudo -u cairnbook /usr/local/bin/cairnbook key list --db /var/lib/cairnbook/cairnbook.db
  ```

  *For a server run in a terminal:*

  ```bash
  cairnbook key list --db ./m2-live-run.db
  ```
- [ ] **5.4 Pick the serve log lines.** Take the server's log lines for the run, and keep
  only the lines that have `msg=listening`, and any with `level=ERROR`. Redact the host
  name or the address if you wish, by replacing it with `<server>`. No key should appear
  in these lines. If one does, say so in your reply and do not paste the line.

  - *For the unit:* `journalctl` needs root, or membership of the `systemd-journal`
    group. Run it as root or with `sudo`:

    ```bash
    sudo journalctl -u cairnbook --no-pager | grep -E 'msg=listening|level=ERROR'
    ```

  - *For a server run in a terminal:* it has no journal entry. Its log is the
    `serve.log` that 1.3 captured with `tee`, so use that file:

    ```bash
    grep -E 'msg=listening|level=ERROR' serve.log
    ```

    If you did not capture it, copy the lines from that terminal's scrollback.

## Paste this back to Bishop

Paste these, in this order, in one message:

1. The output of `cairnbook doctor` (from 3.1, or the block from 5.2).
2. The output of `cairnbook export task <task-id>` (from 5.1, or the block from 5.2).
3. The serve log lines from 5.4, redacted.
4. The output of `cairnbook key list` from 5.3.
5. For a unit only: the output of the three checks from 1.3 (`systemd-analyze verify`,
   `systemctl status cairnbook`, `systemd-analyze security cairnbook`).

Do not paste the key itself, or the contents of `client.env`. The key should appear in
none of them; if it does, say so and leave that line out. The output of `cairnbook key add`
(1.2) prints the key, so leave it out of the paste. In everything you paste, replace any
`cairnbook_...` value with `<redacted>`. If a key is pasted anywhere, revoke it and make a
new one.

## Part 6: clean up

- [ ] **6.1 Revoke the test key** on the server, once the run is pasted back. Use the
  block that matches the run from 1.2 and 1.3. A revoked key stops working at its next
  call, and its name is not reused. Make another key before you revoke this one: a server
  that listens beyond loopback refuses to start with no active key (`RequireKeys`), so a
  restart after the last key is revoked fails. The replacement is made with `key add`, as
  in step 1.2.

  *For the unit:*

  ```bash
  sudo -u cairnbook /usr/local/bin/cairnbook key revoke --db /var/lib/cairnbook/cairnbook.db --name m2-live-run
  ```

  *For a server run in a terminal:*

  ```bash
  cairnbook key revoke --db ./m2-live-run.db --name m2-live-run
  ```
- [ ] **6.2 Remove the client's key file** if the machine is not kept for further runs:
  `rm ~/.config/cairnbook/client.env`. Remove `.mcp.json` from the test project, or, for the
  user-scope route in 2.4, run `claude mcp remove cairnbook --scope user`.
- [ ] **6.3 Stop a terminal server** if you started one for this run. Stop the unit only
  if it was started for this run.

The test task stays in the database. The server has no call to delete a task, so it is
left as history. Closing the run does not close issue #19: the operator does that, when
the output has been checked.
