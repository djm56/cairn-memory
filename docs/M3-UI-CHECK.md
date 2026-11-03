# M3 web UI: hand-test guide

Status as of 2026-11-03: M3's exit check passed in the automated test
(`TestM3ExitCheck`, [internal/server/ui_actions_test.go](../internal/server/ui_actions_test.go)).
This guide is an optional hand check of the same flow, in a real browser, through the
real proxy. It is not needed to close M3, and it changes no status line.

It follows the same shape as [M2-LIVE-RUN.md](M2-LIVE-RUN.md): numbered checkboxes,
exact commands, a list of what to paste back, and cleanup and rollback. It takes about
an hour. The operator runs it and pastes the results back.

Two machines are involved:

- **Server:** Ubuntu 24.04. `cairn serve` runs as the systemd unit `cairn` on
  `127.0.0.1:8788`, with the database at `/var/lib/cairn/cairn.db`. Apache sits in
  front, as in [DEPLOY.md](DEPLOY.md), with TLS from certbot. The guide assumes the
  server runs with `--behind-proxy`. The shipped unit does not pass that flag, so step
  3.3 checks for it and adds it when it is missing.
- **Client:** a Mac with Claude Code and `cairn mcp`, as set up in M2.

The server's name is `cairn.example.net` in this guide, as in DEPLOY.md. Use your own
name in each command, and never paste it back.

Keys used in this run:

| Name | Scope | Where it comes from |
|------|-------|---------------------|
| `<operator-key>` | operator | Already exists: the operator key from M2 |
| `<agent-key>` | agent | The Mac's key from M2 |
| `<viewer-key>` | viewer | Made in step 4.2; you choose the name |

In every command and step, `<...>` stands for your own value. Type the value without
the angle brackets. The key names are yours; this guide does not use them.

## 1. Purpose and scope

The automated test already passes the M3 exit check. This hand check adds what the test
cannot see:

- the real Apache proxy, the TLS and the `Host` and `Origin` handling;
- the headers and the cookie as a browser receives them;
- the sign-in page and the pages as a person reads them;
- the Claude Code side, which talks to the same server.

It changes no code. It writes to the live database in three ways only: the upgrade
(step 3), one test learning and two test directives (step 7), and the test keys and
task (steps 4 and 7). Step 11 cleans up what is left. A failure here is a finding. Stop
at the failing step, record what you saw, and paste it back as step 13 describes.

## 2. Stop and back up (mandatory)

The upgrade in step 3 applies migrations `0006_web_ui.sql` and `0007_web_ui_reads.sql`
to the live database when `cairn serve` starts. Migrations are one-way: the old binary
cannot read the new schema. The backup in this step is **the rollback point**. Step 12
cannot run without it. The server is stopped before the copy is taken, so nothing writes
between the copy and the upgrade. Do not start step 3 until 2.5 has shown the file.

- [ ] **2.1 Make the backups directory, if it does not exist.** It is owner-only and
  owned by the service user, as [DEPLOY.md, "Backup"](DEPLOY.md#backup) sets it:

  ```bash
  sudo install -d -m 0700 -o cairn -g cairn /var/backups/cairn
  ```

- [ ] **2.2 Stop the server.** It stays stopped until step 3.2 starts it.

  ```bash
  sudo systemctl stop cairn
  ```

- [ ] **2.3 Back up the database while the server is stopped.** `cairn backup` makes a
  consistent copy, and refuses to overwrite a file. It opens the database file without
  migrating it, so it works on a stopped server. The command is the one DEPLOY.md gives:

  ```bash
  sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db "/var/backups/cairn/pre-m3-ui-$(date -u +%Y%m%dT%H%M%SZ).db"
  ```

- [ ] **2.4 Confirm the file exists.** It must be non-empty, owned by `cairn`, and
  mode `-rw-------`:

  ```bash
  sudo ls -l /var/backups/cairn/
  ```

- [ ] **2.5 This file is the rollback point.** Write its name down; step 12 names it.
  Keep it until the upgraded server has run cleanly and the check is finished.

## 3. Upgrade

Two routes give the binary, and both build for the server's CPU. Both end with an
`install` into `/usr/local/bin/cairn`, as [DEPLOY.md, "Install the binary"](DEPLOY.md#install-the-binary)
and [its Upgrade section](DEPLOY.md#upgrade) give it. The Makefile's `build` target
stamps the version (`Makefile`, `build:`); route B makes the same stamp by hand.

- **Route A, build on the server.** Needs Go 1.26 and a checkout of `main` on the
  server. From the checkout: `make build`, which writes `bin/cairn`.
- **Route B, build on the Mac for the server's CPU.** Needs Go 1.26 and a checkout of
  `main` on the Mac. On the server, `uname -m` prints `x86_64` (use `amd64`) or
  `aarch64` (use `arm64`). On the Mac, from the checkout, run only the line for that CPU:

  ```bash
  GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(git describe --tags --always --dirty)" -o cairn-linux ./cmd/cairn
  GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=$(git describe --tags --always --dirty)" -o cairn-linux ./cmd/cairn
  ```

  Copy `cairn-linux` to the server with `scp`, in this shape:

  ```bash
  scp cairn-linux <user>@<server>:<remote-dir>/cairn-linux
  ```

  Then install it there in 3.2, from the path you copied it to.

- [ ] **3.1 Keep the old binary, on the server and off it.** DEPLOY.md keeps one copy,
  `cairn.prev`. Look at what is there first, without copying anything yet:

  ```bash
  ls -l /usr/local/bin/cairn /usr/local/bin/cairn.prev
  ```

  - If `cairn.prev` does not exist, copy the current binary to it:

    ```bash
    sudo cp -p /usr/local/bin/cairn /usr/local/bin/cairn.prev
    ```

  - If `cairn.prev` exists, keep it and do not write over it. It may be the rollback
    binary from an earlier upgrade. Copy the current binary to a new name instead:

    ```bash
    sudo cp -p /usr/local/bin/cairn /usr/local/bin/cairn.pre-m3-ui
    ```

    Step 12 then names this file as well as `cairn.prev`.

  Also copy the current `/usr/local/bin/cairn` to the Mac, with `scp`. Step 12 needs
  it if the server's copy is lost.

- [ ] **3.2 Install the new binary, and start.** The server is still stopped from 2.2.
  Use the file from route A or B:

  ```bash
  sudo install -m 0755 bin/cairn /usr/local/bin/cairn
  sudo systemctl start cairn
  ```

  Route B: replace `bin/cairn` with the path where you put the copy, such as
  `~/cairn-linux`.

- [ ] **3.3 Check that the unit runs with `--behind-proxy`.** The shipped unit does not
  pass it: [`docs/deploy/cairn.service:18`](deploy/cairn.service) ends its `ExecStart`
  line at `--addr 127.0.0.1:8788`. Read the line systemd actually runs:

  ```bash
  sudo systemctl cat cairn | grep ExecStart
  ```

  - If the last `ExecStart=` line ends with `--behind-proxy`, go on to 3.4.
  - If it does not, add a drop-in now, before any browser step. Run
    `sudo systemctl edit cairn`. Put these lines in the editor and save. The blank
    `ExecStart=` clears the shipped line, and the next line is the whole command with
    the flag:

    ```ini
    [Service]
    ExecStart=
    ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr 127.0.0.1:8788 --behind-proxy
    ```

    Then reload the unit and restart the server, and read the line again:

    ```bash
    sudo systemctl daemon-reload
    sudo systemctl restart cairn
    sudo systemctl cat cairn | grep ExecStart
    ```

    `systemctl cat` lists the drop-in after the shipped unit, so the last `ExecStart=`
    line is the one that runs. It must now end with `--behind-proxy`.

  **Why this is needed.** The proxy terminates TLS on this host, so the browser sends
  `https://` in each form's `Origin`. Without the flag the server checks forms against
  `http://`. Every form is then refused, sign-in included, and the cookie is not Secure.
  With the flag, the server expects `https://` and sets the cookie as
  `__Secure-cairn_ui`. Do not open the UI until 3.4 passes.

- [ ] **3.4 Check the journal and the service.** Expect a `msg=listening` line, with
  `behind_proxy=true` and `tls=false`, and no `level=ERROR`. If the listening line shows
  `behind_proxy=false`, the drop-in did not take: go back to 3.3.

  ```bash
  sudo systemctl is-active cairn
  sudo systemctl status cairn --no-pager
  sudo journalctl -u cairn -n 30 --no-pager
  ```

  `is-active` must print `active`, and `status` must show `active (running)`. If the
  unit is not running, the migration may have failed: paste the `journalctl` lines and
  stop. Do not work round it.

  The upgrade is confirmed by three things together: `is-active` prints `active`, the
  listening line is as above, and the UI page loads at step 5.1.

- [ ] **3.5 Check the version.**

  ```bash
  /usr/local/bin/cairn version
  ```

  It prints `cairn <version>`. A route-A build shows the `git describe` value; a build
  made without the `-ldflags` stamp shows `dev`.

## 4. Keys

- [ ] **4.1 Reuse `<operator-key>`.** It is the operator key. Check that it, and the
  Mac's `<agent-key>`, are listed and not revoked:

  ```bash
  sudo -u cairn /usr/local/bin/cairn key list --db /var/lib/cairn/cairn.db
  ```

  The list shows names, scopes and times. It never shows a key. If `<operator-key>` is
  missing or revoked, stop: step 5 cannot sign in.

- [ ] **4.2 Make a viewer key.** Choose its name, and put it in place of `<viewer-key>`.
  The key prints once, on stdout:

  ```bash
  sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name <viewer-key> --scope viewer
  ```

  Save the printed key in the password manager now.

- [ ] **4.3 Handle keys with care.** Keys live in the password manager. Never paste a
  key into a chat, a file, a ticket or a command line. Anything that prints a key is
  left out of what you paste back.

## 5. Sign in as operator

- [ ] **5.1 Open the UI.** In a browser, open `https://cairn.example.net/ui/`. Without a
  session you are sent to `/ui/signin`. The page has the heading "Sign in", a field
  labelled "API key", and a "Sign in" button.
- [ ] **5.2 Sign in.** Paste the `<operator-key>` key into the field and press "Sign in".

  **If sign-in fails, read the message on the page first.** A wrong key and a refused
  form look different:

  - **A wrong key** shows "The key was not accepted." at HTTP 401
    ([`internal/ui/signin.go:15`](../internal/ui/signin.go)). The field is empty. Check
    the key in the password manager, and try again. A wrong key writes no `form refused`
    line to the journal.
  - **A refused form** shows "The form could not be verified. Load the page again and
    try again." at HTTP 403 ([`internal/ui/forms.go:18`](../internal/ui/forms.go)). The
    journal then has a WARN line with the message `form refused` and the reason
    `Origin does not match the request's scheme and Host`
    ([`internal/ui/forms.go:65`](../internal/ui/forms.go), [`:74`](../internal/ui/forms.go)).
    This means the server is not running with `--behind-proxy`. Go back to the drop-in
    in step 3.3, check the `ExecStart` line, and sign in again only after 3.3 passes. If
    the journal gives a different `form refused` reason, stop and paste that line.

- [ ] **5.3 Expect the dashboard.** The page heading is "Dashboard". The top bar shows
  the key's name and role, as `<operator-key> (operator)`, followed by "Sign out". The UI
  prints no "Signed in as" text; the name and role in the top bar are the sign-in
  confirmation. Note what you actually see, and say so if it differs.

## 6. Headers and cookie

Use the browser's developer tools. In Chrome, these are the Network and Application
tabs. In Firefox, they are the Network and Storage tabs.

- [ ] **6.1 Check the response headers.** Reload `/ui/`. In the Network tab, select the
  document request for `/ui/`, and read its response headers:

  | Header | Expected | Yes or no |
  |--------|----------|-----------|
  | `Content-Security-Policy` | present; begins `default-src 'none'` | |
  | `X-Content-Type-Options` | `nosniff` | |
  | `Referrer-Policy` | `same-origin` | |
  | `Cache-Control` | `no-store` | |
  | `Strict-Transport-Security` | absent: behind a proxy the server sends none | |

  The fifth row is an absence check. DEPLOY.md and DESIGN.md §5.5 say HSTS is not sent
  behind a proxy, so the header should not appear. If it does appear, record it.

- [ ] **6.2 Check the cookie.** In the Application (or Storage) tab, open Cookies, then
  the site's name. Find the session cookie, and read its attributes:

  | Attribute | Expected | Yes or no |
  |-----------|----------|-----------|
  | Name | `__Secure-cairn_ui` | |
  | HttpOnly | yes | |
  | Secure | yes | |
  | SameSite | Strict | |
  | Path | `/ui` | |

  If the name is `cairn_ui`, with no `__Secure-` prefix, the server is not running with
  `--behind-proxy`. The drop-in in step 3.3 is missing or did not take. Go back to 3.3,
  and check the last `ExecStart=` line with `sudo systemctl cat cairn`. When the flag
  is in place, sign out and sign in again: the old cookie has the old name. Record the
  first result in step 13.

- [ ] **6.3 What to report, and what never to report.** Report the two tables, with
  yes or no in each row. Never paste the cookie's value, and never send a screenshot
  that shows the Value column. Crop it out, or hide it first.

## 7. The exit-check flow by hand

This follows `TestM3ExitCheck` step by step. It uses a test project, `m3-ui-check`,
which the first agent write creates.

- [ ] **7.1 Make the test project on the Mac.** In an empty directory named
  `m3-ui-check`, from the shell:

  ```bash
  mkdir -p m3-ui-check && cd m3-ui-check
  cairn init --project m3-ui-check --server https://cairn.example.net
  ```

  Write `.mcp.json` in this directory, as M2-LIVE-RUN.md 2.4 does, with the absolute
  path that `command -v cairn` prints. The Mac's `~/.config/cairn/client.env` must hold
  the `<agent-key>` key. If it holds another key, put the `<agent-key>` key in its
  place, as M2-LIVE-RUN.md 2.2 does. Never print the file's contents.

  If `cairn hook session-start` in 7.4 reports an unknown event, the Mac's binary
  predates M4. Rebuild it as M2-LIVE-RUN.md 2.1 says.

- [ ] **7.2 Open Claude Code in `m3-ui-check`.** Approve the `cairn` server if asked,
  then run `/mcp` and check that `cairn` is connected.

- [ ] **7.3 Start a task and record a proposed learning.** Paste this into the session:

  > Using the cairn MCP tools, not the shell: start a task titled "M3 UI check" with a
  > one-line goal, using `task_start` with `agent` set to `claude-code`. Leave the task
  > open. Then record one proposed learning with `learning_add`: agent `claude-code`,
  > kind `finding`, title "M3 UI check: a proposed learning", body "A test learning for
  > the M3 hand check." Tell me the task id and the learning id.

  The task stays open. The hook in 7.4 and 7.8 reports directives only while a task is
  active.

- [ ] **7.4 Take the baseline count, before any directive exists.** From the shell, in
  the project directory:

  ```bash
  cairn hook session-start
  ```

  Expect the line `Cairn task: "M3 UI check" ...` and the line `Directives that apply: 0.`
  Record the count. The hook prints a count, not the directive text.

  Read the output with care. The line `Cairn: no task is active in <project>.` means
  no task is active: the task from 7.3 is not open, so go back to 7.3. Empty stdout is
  a different case. If stdout is empty and stderr has a line starting
  `cairn hook session-start: no context added:`, the hook could not read the project.
  Fix that first, and do not read it as "no task".

- [ ] **7.5 See the learning in the browser.** Open
  `https://cairn.example.net/ui/p/m3-ui-check/learnings`, or use the "Learnings" link
  and the project link in the top bar. The default filter shows `proposed` learnings,
  so "M3 UI check: a proposed learning" should appear in the list.

- [ ] **7.6 Accept it with a note.** Open the learning. Under "Decision", choose
  `accepted`. Enter a note in "Note, required". Press "Record decision". Expect a return
  to the project's learnings page. The learning leaves the default `proposed` list. To
  see it again, set the Status filter to `accepted`.

- [ ] **7.7 Ratify a directive from it.** Set the Status filter to `accepted`. On the
  accepted learning, follow "Ratify as a directive". The link appears only on accepted
  learnings. The form says "From learning …". Fill it in:

  - Scope: `project`. An agent-scoped directive is not counted by the CLI hook.
  - Project: `m3-ui-check`.
  - Title: `M3 UI check directive`.
  - Rule: `M3 hand check: this directive tests the ratify path. Retire it at cleanup.`
  - Review by: leave blank. It then falls 90 days out.

  Press "Ratify". Expect the directives page, with the new directive listed as active.
  Note its code, for example `D-0001`. The learning's status is now `applied`, which
  you can see with the Status filter.

- [ ] **7.8 Check the directive in a new session.** Quit Claude Code and open it again in
  `m3-ui-check`.

  - From the shell, run `cairn hook session-start` again. Expect `Directives that apply: 1.`
    The count went from 0 to 1. If it prints `Cairn: no task is active`, the task from
    7.3 is not open. Stop and record that; do not start a new task to move the count.
    Empty stdout with a `no context added:` line on stderr is a read failure, as in 7.4.
  - In Claude Code, paste: "Call `task_current` with the cairn MCP tool and show me the
    directives it returns." Expect `M3 UI check directive` in the result.

  The hook counts only; `task_current` gives the text. This is the same check
  `TestM3ExitCheck` makes.

- [ ] **7.9 Reaffirm and retire a throwaway directive.** Ratify a second directive with
  no learning behind it: open `/ui/directives/new`, and set Scope `project`, Project
  `m3-ui-check`, Title `M3 UI check throwaway`, Rule `Throwaway for the M3 hand check.
  Retire it.` Press "Ratify".

  - **Reaffirm.** On the directives page, find the throwaway. Its Reaffirm form offers
    a date 90 days out. Change it to another date, and press "Reaffirm". Expect the
    review date on the page to change.
  - **Retire.** Tick "Confirm", which the page describes as final, and press "Retire".
    Expect the directive to leave the Active list.
  - **Status.** Open `/ui/directives?state=retired`. Expect the throwaway, with "retired"
    and a time.

  The UI shows the current review date and state. It does not show the reaffirmation
  history, which `DirectiveReaffirm` writes to the `directive_reaffirmations` table
  (migration 0006). No page or command in this guide reads that table. So for the
  history, report "not visible in the UI", and do not query the database for it.

## 8. Viewer refusal

- [ ] **8.1 Sign out.** Press "Sign out" in the top bar. Expect the sign-in page.
- [ ] **8.2 Sign in with the viewer key.** Paste `<viewer-key>`. Expect the dashboard, with
  `<viewer-key> (viewer)` in the top bar.
- [ ] **8.3 Visit every page, and look for forms.** Open each of these, and confirm that
  none of them shows an action:

  - `/ui/`
  - `/ui/tasks`
  - `/ui/p/m3-ui-check/tasks/<task id>`, with the task id from 7.3
  - `/ui/p/m3-ui-check/learnings`
  - `/ui/learnings`
  - `/ui/directives`, and `/ui/directives?state=retired`
  - `/ui/projects`
  - `/ui/projects/m3-ui-check/settings`

  Expect no "Ratify a directive" button, no Reaffirm or Retire form, no decision form and
  no settings form. To check it rather than trust the eye, open the page source
  (View Source, or Cmd-Option-U or Ctrl-U) and search for `<form`. Expect no match. The
  "Sign out" link is a link, not a form.
- [ ] **8.4 Refusal of the ratify form.** Open `/ui/directives/new`. Expect HTTP 403, and
  the text "This key is read-only." Read the status in the Network tab.

## 9. Agent key refused at sign-in

- [ ] **9.1 Sign out** of the viewer session. Expect the sign-in page.
- [ ] **9.2 Paste the agent key.** Paste `<agent-key>` from the password manager into the
  "API key" field, and press "Sign in".

  If `<agent-key>` is not in the password manager, do not look for it elsewhere. Either
  make a temporary agent key here, and revoke it in step 11.3, or skip step 9 and give
  the reason in step 13. To make a temporary key, choose its name in place of
  `<temp-agent-key>`:

  ```bash
  sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name <temp-agent-key> --scope agent
  ```

  Save the printed key in the password manager, then paste it here. It prints once.

- [ ] **9.3 Expect a refusal.** The sign-in page comes back, with HTTP 401 in the Network
  tab, and the message "This is an agent key. The web UI signs in operator and viewer
  keys." The key field is empty. The key must not appear anywhere on the page. If it
  does, that is a finding: stop, and do not paste the page.

## 10. Sign out

- [ ] **10.1 Sign in, then sign out.** Sign in with the operator or the viewer key. Press
  "Sign out". Expect the sign-in page. In the Network tab, the sign-out response clears
  the cookie with `Max-Age=0`.
- [ ] **10.2 Check that the UI redirects.** Open `/ui/` directly. Expect a redirect to
  `/ui/signin`.

## 11. Cleanup

Nothing here is deleted. Directives are retired, never removed, and a learning is never
removed from the list. Each step is the operator's choice; a step left out is recorded
in step 13.

- [ ] **11.1 Sign in with `<operator-key>`, and retire the test directive,** `M3 UI check
  directive`, from the directives page, with "Confirm" ticked. Retiring is final. Skip
  this step if the operator wants to keep it. Sign in first: step 10 ended signed out,
  and the directives page needs an operator session.
- [ ] **11.2 Revoke `<viewer-key>`,** unless the operator wants to keep it:

  ```bash
  sudo -u cairn /usr/local/bin/cairn key revoke --db /var/lib/cairn/cairn.db --name <viewer-key>
  ```

  A revoked key stops working at its next call. Its name is not reused.

- [ ] **11.3 Revoke the temporary agent key,** only if step 9.2 made one:

  ```bash
  sudo -u cairn /usr/local/bin/cairn key revoke --db /var/lib/cairn/cairn.db --name <temp-agent-key>
  ```

- [ ] **11.4 Leave the test learning.** It stays as history, with the status `applied`.
  Nothing in the UI removes a learning.
- [ ] **11.5 Close the test task on the Mac.** Ask Claude Code to call `task_close` with
  outcome done and a one-line summary. If the close is refused, the reply lists what is
  missing. Supply it, then close again.
- [ ] **11.6 Remove the Mac's `.mcp.json`** from `m3-ui-check` if the machine is not kept
  for further runs.
- [ ] **11.7 Keep the step 2 backup** until the upgraded server has run cleanly. Deleting
  it is the operator's decision.

## 12. Rollback

Use this if the upgrade fails or the server does not behave. Restoring the old binary
alone is not enough, because the old binary cannot read the migrated schema. Put back
both the old binary and the backup from step 2. DEPLOY.md, "Upgrade", gives the same
order.

- [ ] **12.1 Stop the service.**

  ```bash
  sudo systemctl stop cairn
  ```

- [ ] **12.2 Put back the previous binary.** Use the copy that holds the build which ran
  before this upgrade. That is `cairn.prev`, or `cairn.pre-m3-ui` if step 3.1 made that
  copy. If neither is on the server, use the copy kept on the Mac in step 3.1. The
  example uses `cairn.prev`:

  ```bash
  sudo install -m 0755 /usr/local/bin/cairn.prev /usr/local/bin/cairn
  ```

  **The drop-in from step 3.3 stays in place.** A binary rollback does not remove it,
  and it does not need to. The old binary accepts the flag too: `--behind-proxy` was
  added before M2 closed, so every build from M2 on has it. Check on the server, which
  prints the flags and opens no database:

  ```bash
  /usr/local/bin/cairn.prev serve --help | grep behind-proxy
  ```

  The line should print. Source: [`cmd/cairn/serve.go:32`](../cmd/cairn/serve.go); the
  flag came in with commit `419a7ba`, which is in the M2 close `b3d7467`.

  To remove the drop-in, run `sudo systemctl revert cairn`, then
  `sudo systemctl daemon-reload` and `sudo systemctl restart cairn`. The revert removes
  every drop-in for `cairn`, not only this one, so run `sudo systemctl cat cairn` first
  and note any other drop-in. Use it only when the server no longer sits behind a TLS
  proxy on this host, for example after a move to the SSH tunnel or to setup (a). On
  plain HTTP the flag makes the server expect `https://`, and every form is then refused.

- [ ] **12.3 Restore the step 2 backup.** Run this from an interactive terminal. The
  restore asks you to type the database path to confirm.

  ```bash
  sudo -u cairn /usr/local/bin/cairn restore --db /var/lib/cairn/cairn.db /var/backups/cairn/<the step 2 file name>
  ```

  Restore never deletes anything. It moves the current database aside to
  `/var/lib/cairn/cairn.db.pre-restore-<UTC time>`. Anything written since the upgrade
  started is in that moved-aside file, not in the restored database. Keep it until the
  restored server checks out, then delete it by hand, as DEPLOY.md says.

- [ ] **12.4 Start the service, and check the journal.**

  ```bash
  sudo systemctl start cairn
  sudo journalctl -u cairn -n 20 --no-pager
  ```

  The listening line shows `behind_proxy=true` if the drop-in is still in place.

## 13. What to paste back

Paste these, in this order, in one message:

1. The `msg=listening` line from 3.4, and any line with `level=ERROR`.
2. The output of `cairn version` (3.5).
3. The two yes-or-no tables from 6.1 and 6.2.
4. Whether the `ExecStart` line already had `--behind-proxy` at 3.3, and if the drop-in
   was added, that the listening line then showed `behind_proxy=true`.
5. The status of each step from 2 to 11: done, skipped (with the reason), or failed (with
   what you saw).
6. From step 5: what the sign-in page and the top bar showed, and for any failure, the
   page message and the status.
7. From step 7: the two `Directives that apply:` counts (0, then 1), whether
   `task_current` returned the directive, and what the throwaway's reaffirm and retire
   showed. Say whether any history was visible.
8. From step 8: whether any page had a form, and the status of the refusal in 8.4.
9. From step 9: the status and message of the refusal in 9.3, and whether a temporary
   agent key was used.
10. If 6.2 did not show `__Secure-cairn_ui`, after the drop-in: the `ExecStart` line from
    `sudo systemctl cat cairn`.

Never paste:

- a key of any kind (the operator, agent or viewer key, under whatever name you gave it),
  or the output of `key add`;
- a cookie, or its value;
- a CSRF value (the hidden `csrf` field in a page's source);
- the server's hostname. Write `<server>` in its place;
- anything from the password manager.

If a key is pasted anywhere, revoke it and make a new one, as M2-LIVE-RUN.md does. If a
cookie value is pasted, sign out in that browser.
