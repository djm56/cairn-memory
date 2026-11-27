# Upgrade

_Status as of 2026-11-27: M5 (Plugin) is current; the tool is renamed to Cairnbook (D38). M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at a33efd5. The old-prefix key fallback (keys that start `cairn_`) is not in a33efd5; it is in the commit after it._

This page upgrades a server that is already running, and a local install. The database schema is the part that needs care. A newer build changes the database when it starts, and an older build refuses to open a database that a newer one has changed. So the rollback path is the pre-upgrade backup, not the old binary alone.

Upgrade the server before its clients. [DESIGN.md §5.4](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#54-the-http-wire-form) gives that rule. The code does not enforce it.

## Quick version: a server under systemd

Run as root, unless a line says `sudo -u cairnbook`. The new binary is the file you built or copied in [Install-Server](Install-Server), Part 1. Use its full path. The example below uses `/home/you/cairnbook-linux`, where `you` is your SSH user.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
/usr/local/bin/cairnbook version && \
install -d -m 0700 -o cairnbook -g cairnbook /var/backups/cairnbook && \
systemctl stop cairnbook && \
cp -p /usr/local/bin/cairnbook /usr/local/bin/cairnbook.prev-$STAMP && \
sudo -u cairnbook /usr/local/bin/cairnbook backup --db /var/lib/cairnbook/cairnbook.db /var/backups/cairnbook/pre-upgrade-$STAMP.db && \
ls -l /var/backups/cairnbook/pre-upgrade-$STAMP.db && \
install -m 0755 /home/you/cairnbook-linux /usr/local/bin/cairnbook && \
systemctl start cairnbook
```

**Stop if any line fails.** From `version` to `systemctl start cairnbook` the lines are joined with `&&`, so each one runs only when the one before it succeeded. If any line prints an error, nothing after it has run, and the old binary is still installed. If the error came after the server was stopped, `systemctl start cairnbook` starts the old build again, on a database that no migration has touched. Do not run the `install` line by hand to get past a failed backup.

If `systemctl start cairnbook` fails after the install, the new binary is already in place, so do not install it again. Read the journal, then follow [step 6](#6-start-the-server) for what a failed start means, and [step 9](#9-roll-back) for the rollback.

Then verify:

```bash
/usr/local/bin/cairnbook version
journalctl -u cairnbook -n 20
curl -sS https://<host>:8788/healthz
```

**You should see** the new `cairnbook <hash>` from `version`, a `listening` line in the journal with `tls=` or `behind_proxy=` as before, and `{"status":"ok"}` from `healthz`. For setup (b), use the proxy's address in place of `https://<host>:8788`.

The first line of the block makes `/var/backups/cairnbook`, owned by `cairnbook`, mode 0700. It is the same command as [Operations, Backup](Operations#backup) and [DEPLOY.md, Backup](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#backup). It runs without `sudo` because the block runs as root. Install-Server, Part 2, makes the folder on a fresh install. Running the line again does not touch the files in it.

## Detailed version

The stamp in step 1 names every file this upgrade makes. Use the same value in every later command, in the same shell.

### 1. Check the current version and set the stamp

The version is the one the binary prints. `/healthz` does not show it.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
/usr/local/bin/cairnbook version
/home/you/cairnbook-linux version
```

The first version line is what runs now. The second is the new build. A binary built with a plain `go build` prints `cairnbook dev`. The version strings are in [Binaries](Binaries).

Read the release notes for the new build in the [CHANGELOG](https://github.com/djm56/cairn-memory/blob/main/CHANGELOG.md) before you go on. A build can change the schema, the wire form or the commands.

### 2. Make the backup folder and stop the server

```bash
install -d -m 0700 -o cairnbook -g cairnbook /var/backups/cairnbook
systemctl stop cairnbook
```

If the first line prints an error, stop here. The server is still running and nothing has changed.

Stop first, then back up. `cairnbook backup` takes a consistent copy while the server runs, but a write made between a backup taken while the server is up and the stop would be missing from a rollback. A backup taken after the stop holds every write up to the upgrade.

### 3. Keep the previous binary

```bash
cp -p /usr/local/bin/cairnbook /usr/local/bin/cairnbook.prev-$STAMP
```

The name carries the stamp, so running the upgrade again does not overwrite the copy of the build you were running. Keep it under `/usr/local/bin`, not in your home directory. `make push` copies over `~/cairnbook-linux` each time it runs, so the copy in your home directory is not a safe record of the old build.

If this line prints an error, start the server again with `systemctl start cairnbook`, and go no further.

### 4. Back up the database

```bash
sudo -u cairnbook /usr/local/bin/cairnbook backup --db /var/lib/cairnbook/cairnbook.db /var/backups/cairnbook/pre-upgrade-$STAMP.db
ls -l /var/backups/cairnbook/pre-upgrade-$STAMP.db
```

**Gate.** Do not go on unless the backup line printed no error and `ls -l` lists the file. If the backup prints an error, the server is stopped and the database is unchanged. Start the server again with `systemctl start cairnbook`, which runs the old build, and fix the cause before you try again.

`backup` writes a new file at mode 0600, and refuses a name that already exists. It opens the database **without** migrating it, so the copy keeps the schema it had. Keep this file until the new server has run cleanly.

### 5. Install the new binary

Install only after the `ls -l` in step 4 has listed the backup file. The install does not change the database. The first start in step 6 is the first point at which it changes.

```bash
install -m 0755 /home/you/cairnbook-linux /usr/local/bin/cairnbook
```

For a server you built on the server itself, the file is `bin/cairnbook` in the checkout, and the command is `install -m 0755 bin/cairnbook /usr/local/bin/cairnbook`. On a server that received its binary with `make push`, the file is `/home/you/cairnbook-linux`, not `bin/cairnbook`. Use the file you copied.

### 6. Start the server

```bash
systemctl start cairnbook
journalctl -u cairnbook -n 20
```

**The schema changes here.** On start, the server opens the database and applies each migration above the database's current version, in order, each in its own transaction. This build has migrations 0001 to 0007: the schema, indexes and guards, the events project guard, API keys, the directive `review_by` date (with its 90-day backfill), and the web UI tables and reads. A database that is already at 7 is opened without a change.

If a migration fails, the migrations before it stay applied, because each one has its own transaction. The server does not start, and the journal names the error. Do not run the new binary further. Roll back, as step 9 shows.

**Key commands also migrate.** `cairnbook key add`, `cairnbook key list` and `cairnbook key revoke` open the database with the same migrator. Running `cairnbook key list` with a new build against an old database upgrades that database, even though the command only reads. Run key commands with the new binary only when you are ready to upgrade.

### 7. Verify

```bash
/usr/local/bin/cairnbook version
systemctl status cairnbook
journalctl -u cairnbook -n 20
curl -sS https://<host>:8788/healthz
```

Then, from a client that has a key, run `cairnbook doctor`, and [Checks](Checks)' operator check against the server:

```bash
CAIRNBOOK_API_KEY="$(sed -n 's/^CAIRNBOOK_API_KEY=//p' ~/.config/cairnbook/client.env)" \
  <path to the clone>/scripts/smoke-check.sh --server https://<host>:8788
```

**You should see** `active (running)` from `status`, the `listening` line from the journal, `{"status":"ok"}` from `healthz`, and seven `PASS` lines from the check. If `doctor` shows `reach` as `fail`, see [Troubleshooting](Troubleshooting).

Keep `cairnbook.prev-<UTC time>` and the pre-upgrade backup until the new server has run cleanly for as long as you need. `<UTC time>` is the stamp from step 1.

### 8. Upgrade the clients

Each client with a local binary upgrades its own copy. See [Upgrading a local install](#upgrading-a-local-install). A client holds no database, so nothing migrates on a client.

### 9. Roll back

Rolling back needs both the old binary and the pre-upgrade backup. The old binary cannot read the new schema, so it cannot run on the upgraded database. Use the stamp from step 1. If you no longer have it, `ls /usr/local/bin/cairnbook.prev-* /var/backups/cairnbook/pre-upgrade-*` lists the names.

```bash
systemctl stop cairnbook
install -m 0755 /usr/local/bin/cairnbook.prev-<UTC time> /usr/local/bin/cairnbook
sudo -u cairnbook /usr/local/bin/cairnbook restore --db /var/lib/cairnbook/cairnbook.db /var/backups/cairnbook/pre-upgrade-<UTC time>.db
systemctl start cairnbook
journalctl -u cairnbook -n 20
```

`restore` must run from an interactive shell, as the `cairnbook` user, because it asks you to type the database path. A script or a pipe is refused. It stops with `stdin is not a terminal` and changes nothing.

`restore` moves the upgraded database aside to `/var/lib/cairnbook/cairnbook.db.pre-restore-<UTC time>`. It never deletes anything. Anything written after the upgrade is in that file, not in the restored one. Keep it until you have checked the restored server, and delete it yourself.

[DEPLOY.md, Restore](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#restore) gives the full rules, including what to do when a restore stops partway.

## Can a newer schema run on an older binary?

**No.** The older build refuses. Its migrator reads the schema version, and when the database is newer than the build it stops with `database is at schema version N, newer than this build (M)`. That check runs on every command that opens the database. It was tested on a copy of a database at version 8, with a build at 7: `cairnbook key list` and `cairnbook key add` both refused, with exit code 1.

`cairnbook backup` and `cairnbook restore` refuse the same database with a slightly different line: `store: <path> is at schema version N, newer than this build (M)`. After an upgrade, take backups with the new binary. The old one cannot take a copy of a newer database.

A backup from a newer build is refused by `restore` too, because `restore` checks the schema version before it moves anything.

So the old binary is never enough on its own. Put it back together with the backup taken before the upgrade, which has the old schema.

## Upgrading a local install

A local install has no server and no database, so there is no migration to think about.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
cd cairn-memory
git pull
make build
if [ -f ~/.local/bin/cairnbook ]; then cp -p ~/.local/bin/cairnbook ~/.local/bin/cairnbook.prev-$STAMP; fi
mkdir -p ~/.local/bin
install -m 0755 bin/cairnbook ~/.local/bin/cairnbook
cairnbook version
cd ~/my-project   # your project folder, the one with .cairnbook.toml
cairnbook doctor
```

The `cp` line keeps the old binary when there is one. On a first install there is no `~/.local/bin/cairnbook` to keep, so the guard skips the copy.

**You should see** the new hash from `cairnbook version`, and `doctor` with `ok` on each line. Run `doctor` from the project folder, the one that holds `.cairnbook.toml`.

If you also run `cairnbook serve` on this machine, stop it first, back up its database, then start the new build on the same `--db`. The steps are those of the server upgrade above, with your own paths. Back up with `cairnbook backup --db <file> <out>`, and keep the old binary as `cairnbook.prev-<UTC time>` beside the new one.

## From Cairn to Cairnbook

On 2026-11-27 the tool was renamed from Cairn to Cairnbook (decision D38). The old settings and files are still read when the new ones are missing, so an install made under the old name keeps working once you install the new binary. The rows marked No below need action; move to the new names when you can.

| Old | New | Still read? |
|---|---|---|
| `cairn` (the binary) | `cairnbook` | No: install the new binary under its new name |
| `CAIRN_SERVER`, `CAIRN_API_KEY`, `CAIRN_PROJECT`, `CAIRN_AGENT` | `CAIRNBOOK_SERVER`, `CAIRNBOOK_API_KEY`, `CAIRNBOOK_PROJECT`, `CAIRNBOOK_AGENT` | Yes, when the new one is unset or empty |
| `.cairn.toml` | `.cairnbook.toml` | Yes. In each folder the new name is read first, and the nearest folder wins: a `.cairn.toml` in a nearer folder beats a `.cairnbook.toml` further up |
| `~/.config/cairn/client.env`, with a `CAIRN_API_KEY=` line | `~/.config/cairnbook/client.env`, with a `CAIRNBOOK_API_KEY=` line | Yes. The old path is read only when the new file does not exist. Inside a file, a `CAIRNBOOK_API_KEY` line wins over a `CAIRN_API_KEY` line |
| An API key with the prefix `cairn_` | An API key with the prefix `cairnbook_` | Yes: a key issued under the old name keeps working, and new keys start with `cairnbook_` |
| `X-Cairn-*` request headers | `X-Cairnbook-*` | Yes, by the server. An old header is read only when no `X-Cairnbook-*` header of that kind is present, even an empty one |
| MCP server `cairn` (tools `mcp__cairn__*`) | `cairnbook` (tools `mcp__cairnbook__*`) | No: re-register it (below) |
| Plugin `cairn@cairn-memory` | `cairnbook@cairnbook` | No: reinstall it (below) |
| UI cookie `cairn_ui` | `cairnbook_ui` | No: sign in to the web UI again |

**On each machine:**

```bash
cd cairn-memory
git pull
make build
install -m 0755 bin/cairnbook ~/.local/bin/cairnbook
mkdir -p -m 700 ~/.config/cairnbook
[ -f ~/.config/cairn/client.env ] && [ ! -f ~/.config/cairnbook/client.env ] && (umask 077 && sed 's/^\(export \)\{0,1\}CAIRN_API_KEY=/CAIRNBOOK_API_KEY=/' ~/.config/cairn/client.env > ~/.config/cairnbook/client.env)
```

Then rename any `CAIRN_*` variables in your shell profile to `CAIRNBOOK_*`. When `cairnbook doctor` passes from a project, check the new key file before you remove anything:

```bash
ls -l ~/.config/cairnbook/client.env
```

**You should see** `-rw-------` on that line. If the file is missing, or the mode is anything else, keep the old key file and check the new one first. Otherwise remove the old binary (`~/.local/bin/cairn`) and the old key file.

**In each project:**

```bash
git mv .cairn.toml .cairnbook.toml
cairnbook enable --agent claude-code
cairnbook doctor --agent claude-code
```

`enable` renames the hooks an older `cairn enable` wrote (`cairn hook ...` becomes `cairnbook hook ...`) in place. For OpenCode and pi it removes the old `cairn` MCP entry and registers `cairnbook`; delete the old `.cairn/` folder, and for pi the old `<!-- cairn:begin` block in `AGENTS.md`, by hand. If you added the server with `claude mcp add`, remove the old entry with `claude mcp remove cairn` and add it again as `cairnbook`. Allow-lists that name `mcp__cairn__*` tools need the new prefix.

**The Claude Code plugin:** uninstall `cairn@cairn-memory`, then install `cairnbook@cairnbook` as [Install-Plugin](Install-Plugin) shows. `enable` and `doctor` warn while the old plugin is still enabled.

**A server:** upgrade the server before its clients, as the top of this page says. A server on this build reads either header, so once it is upgraded the clients can move to the new names in any order. Until then, an older server does not read the `X-Cairnbook-*` headers that a client under the new name sends, so the server comes first.

The systemd unit in `docs/deploy` now runs `/usr/local/bin/cairnbook` as the user `cairnbook` on `/var/lib/cairnbook/cairnbook.db`. An existing server can keep its old unit and paths: install the new binary, point the unit's `ExecStart` at it, and leave `--db` as it is. To move to the new paths, stop the server, take a backup, move the state directory and the database file to their new names, update the unit, and start it again, as in the steps above.

A server still set up under the old names needs a backup before you swap the binary. Run these with the old names: the user `cairn`, the unit `cairn`, and the paths it was set up with. They mirror steps 2 and 4 above:

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
install -d -m 0700 -o cairn -g cairn /var/backups/cairn
systemctl stop cairn
sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db /var/backups/cairn/pre-upgrade-$STAMP.db
ls -l /var/backups/cairn/pre-upgrade-$STAMP.db
```

**Gate.** Install the new binary only after the `ls -l` lists the backup file. If any line fails, start the old server again with `systemctl start cairn`, which runs the old build, and fix the cause first.

## Versions of this page

This page describes the code at a33efd5, with the old-prefix key fallback of the commit after it. The migrations, the commands and the refusals are the ones in that build. When a newer build changes them, this page changes with the code, in the same pull request.
