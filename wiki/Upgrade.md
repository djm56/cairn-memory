# Upgrade

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da._

This page upgrades a server that is already running, and a local install. The database schema is the part that needs care. A newer build changes the database when it starts, and an older build refuses to open a database that a newer one has changed. So the rollback path is the pre-upgrade backup, not the old binary alone.

Upgrade the server before its clients. [DESIGN.md §5.4](https://github.com/djm56/cairn-memory/blob/main/docs/DESIGN.md#54-the-http-wire-form) gives that rule. The code does not enforce it.

## Quick version: a server under systemd

Run as root, unless a line says `sudo -u cairn`. The new binary is the file you built or copied in [Install-Server](Install-Server), Part 1. Use its full path. The example below uses `/home/you/cairn-linux`, where `you` is your SSH user.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
/usr/local/bin/cairn version && \
install -d -m 0700 -o cairn -g cairn /var/backups/cairn && \
systemctl stop cairn && \
cp -p /usr/local/bin/cairn /usr/local/bin/cairn.prev-$STAMP && \
sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db /var/backups/cairn/pre-upgrade-$STAMP.db && \
ls -l /var/backups/cairn/pre-upgrade-$STAMP.db && \
install -m 0755 /home/you/cairn-linux /usr/local/bin/cairn && \
systemctl start cairn
```

**Stop if any line fails.** From `version` to `systemctl start cairn` the lines are joined with `&&`, so each one runs only when the one before it succeeded. If any line prints an error, nothing after it has run, and the old binary is still installed. If the error came after the server was stopped, `systemctl start cairn` starts the old build again, on a database that no migration has touched. Do not run the `install` line by hand to get past a failed backup.

Then verify:

```bash
/usr/local/bin/cairn version
journalctl -u cairn -n 20
curl -sS https://<host>:8788/healthz
```

**You should see** the new `cairn <hash>` from `version`, a `listening` line in the journal with `tls=` or `behind_proxy=` as before, and `{"status":"ok"}` from `healthz`. For setup (b), use the proxy's address in place of `https://<host>:8788`.

The first line of the block makes `/var/backups/cairn`, owned by `cairn`, mode 0700. It is the same command as [Operations, Backup](Operations#backup) and [DEPLOY.md, Backup](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#backup). It runs without `sudo` because the block runs as root. Install-Server, Part 2, makes the folder on a fresh install. Running the line again does not touch the files in it.

## Detailed version

The stamp in step 1 names every file this upgrade makes. Use the same value in every later command, in the same shell.

### 1. Check the current version and set the stamp

The version is the one the binary prints. `/healthz` does not show it.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
/usr/local/bin/cairn version
/home/you/cairn-linux version
```

The first version line is what runs now. The second is the new build. A binary built with a plain `go build` prints `cairn dev`. The version strings are in [Binaries](Binaries).

Read the release notes for the new build in the [CHANGELOG](https://github.com/djm56/cairn-memory/blob/main/CHANGELOG.md) before you go on. A build can change the schema, the wire form or the commands.

### 2. Make the backup folder and stop the server

```bash
install -d -m 0700 -o cairn -g cairn /var/backups/cairn
systemctl stop cairn
```

If the first line prints an error, stop here. The server is still running and nothing has changed.

Stop first, then back up. `cairn backup` takes a consistent copy while the server runs, but a write made between a backup taken while the server is up and the stop would be missing from a rollback. A backup taken after the stop holds every write up to the upgrade.

### 3. Keep the previous binary

```bash
cp -p /usr/local/bin/cairn /usr/local/bin/cairn.prev-$STAMP
```

The name carries the stamp, so running the upgrade again does not overwrite the copy of the build you were running. Keep it under `/usr/local/bin`, not in your home directory. `make push` copies over `~/cairn-linux` each time it runs, so the copy in your home directory is not a safe record of the old build.

If this line prints an error, start the server again with `systemctl start cairn`, and go no further.

### 4. Back up the database

```bash
sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db /var/backups/cairn/pre-upgrade-$STAMP.db
ls -l /var/backups/cairn/pre-upgrade-$STAMP.db
```

**Gate.** Do not go on unless the backup line printed no error and `ls -l` lists the file. If the backup prints an error, the server is stopped and the database is unchanged. Start the server again with `systemctl start cairn`, which runs the old build, and fix the cause before you try again.

`backup` writes a new file at mode 0600, and refuses a name that already exists. It opens the database **without** migrating it, so the copy keeps the schema it had. Keep this file until the new server has run cleanly.

### 5. Install the new binary

Install only after the `ls -l` in step 4 has listed the backup file. The install does not change the database. The first start in step 6 is the first point at which it changes.

```bash
install -m 0755 /home/you/cairn-linux /usr/local/bin/cairn
```

For a server you built on the server itself, the file is `bin/cairn` in the checkout, and the command is `install -m 0755 bin/cairn /usr/local/bin/cairn`. On a server that received its binary with `make push`, the file is `/home/you/cairn-linux`, not `bin/cairn`. Use the file you copied.

### 6. Start the server

```bash
systemctl start cairn
journalctl -u cairn -n 20
```

**The schema changes here.** On start, the server opens the database and applies each migration above the database's current version, in order, each in its own transaction. This build has migrations 0001 to 0007: the schema, indexes and guards, the events project guard, API keys, the directive `review_by` date (with its 90-day backfill), and the web UI tables and reads. A database that is already at 7 is opened without a change.

If a migration fails, the migrations before it stay applied, because each one has its own transaction. The server does not start, and the journal names the error. Do not run the new binary further. Roll back, as step 9 shows.

**Key commands also migrate.** `cairn key add`, `cairn key list` and `cairn key revoke` open the database with the same migrator. Running `cairn key list` with a new build against an old database upgrades that database, even though the command only reads. Run key commands with the new binary only when you are ready to upgrade.

### 7. Verify

```bash
/usr/local/bin/cairn version
systemctl status cairn
journalctl -u cairn -n 20
curl -sS https://<host>:8788/healthz
```

Then, from a client that has a key, run `cairn doctor`, and [Checks](Checks)' operator check against the server:

```bash
CAIRN_API_KEY="$(sed -n 's/^CAIRN_API_KEY=//p' ~/.config/cairn/client.env)" \
  <path to the clone>/scripts/smoke-check.sh --server https://<host>:8788
```

**You should see** `active (running)` from `status`, the `listening` line from the journal, `{"status":"ok"}` from `healthz`, and seven `PASS` lines from the check. If `doctor` shows `reach` as `fail`, see [Troubleshooting](Troubleshooting).

Keep `cairn.prev-<UTC time>` and the pre-upgrade backup until the new server has run cleanly for as long as you need. `<UTC time>` is the stamp from step 1.

### 8. Upgrade the clients

Each client with a local binary upgrades its own copy. See [Upgrading a local install](#upgrading-a-local-install). A client holds no database, so nothing migrates on a client.

### 9. Roll back

Rolling back needs both the old binary and the pre-upgrade backup. The old binary cannot read the new schema, so it cannot run on the upgraded database. Use the stamp from step 1. If you no longer have it, `ls /usr/local/bin/cairn.prev-* /var/backups/cairn/pre-upgrade-*` lists the names.

```bash
systemctl stop cairn
install -m 0755 /usr/local/bin/cairn.prev-<UTC time> /usr/local/bin/cairn
sudo -u cairn /usr/local/bin/cairn restore --db /var/lib/cairn/cairn.db /var/backups/cairn/pre-upgrade-<UTC time>.db
systemctl start cairn
journalctl -u cairn -n 20
```

`restore` must run from an interactive shell, as the `cairn` user, because it asks you to type the database path. A script or a pipe is refused. It stops with `stdin is not a terminal` and changes nothing.

`restore` moves the upgraded database aside to `/var/lib/cairn/cairn.db.pre-restore-<UTC time>`. It never deletes anything. Anything written after the upgrade is in that file, not in the restored one. Keep it until you have checked the restored server, and delete it yourself.

[DEPLOY.md, Restore](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#restore) gives the full rules, including what to do when a restore stops partway.

## Can a newer schema run on an older binary?

**No.** The older build refuses. Its migrator reads the schema version, and when the database is newer than the build it stops with `database is at schema version N, newer than this build (M)`. That check runs on every command that opens the database. It was tested on a copy of a database at version 8, with a build at 7: `cairn key list` and `cairn key add` both refused, with exit code 1.

`cairn backup` and `cairn restore` refuse the same database with a slightly different line: `store: <path> is at schema version N, newer than this build (M)`. After an upgrade, take backups with the new binary. The old one cannot take a copy of a newer database.

A backup from a newer build is refused by `restore` too, because `restore` checks the schema version before it moves anything.

So the old binary is never enough on its own. Put it back together with the backup taken before the upgrade, which has the old schema.

## Upgrading a local install

A local install has no server and no database, so there is no migration to think about.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
cd cairn-memory
git pull
make build
cp -p ~/.local/bin/cairn ~/.local/bin/cairn.prev-$STAMP
install -m 0755 bin/cairn ~/.local/bin/cairn
cairn version
cairn doctor
```

**You should see** the new hash from `cairn version`, and `doctor` with `ok` on each line.

If you also run `cairn serve` on this machine, stop it first, back up its database, then start the new build on the same `--db`. The steps are those of the server upgrade above, with your own paths. Back up with `cairn backup --db <file> <out>`, and keep the old binary as `cairn.prev-<UTC time>` beside the new one.

## Versions of this page

This page describes the code at 8b651da. The migrations, the commands and the refusals are the ones in that build. When a newer build changes them, this page changes with the code, in the same pull request.
