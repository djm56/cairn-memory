# Quick update: a test server to the latest main

_Status as of 2026-11-11, checked against main at time of writing. Not run end to end when this was written._

This is the fast path for an install made with [QUICK-INSTALL.md](QUICK-INSTALL.md). Replace `<repo>` (your cairn-memory checkout) and `<project>` (the project folder) before you paste. The wiki is the full reference: [Upgrade](../wiki/Upgrade.md) explains each step and the schema rules. Upgrade the server before its clients.

**Warning.** Do not run any `cairnbook key` command with the new binary until you are ready to upgrade. `key list` also opens the database and migrates it. The first `cairnbook serve` on the new build migrates it too.

## 1. Note the version and build the new one

The old server keeps running for this step.

```bash
cairnbook version
cd <repo>
git switch main && git pull --ff-only
make build && bin/cairnbook version
```

The first `cairnbook version` is the old version. The last one is the new build from main: expect its short hash. If that hash ends in `-dirty`, stop, because the checkout has local changes. If the build fails, stop. Nothing has changed, and the server is still running.

## 2. Stop the server and back up with the old binary

Press Ctrl-C in the server terminal. Then, in the first terminal, stay in the same shell for the rest of the update, because the rollback uses `STAMP`. Stop if any line fails. The server is stopped and the database is unchanged: start the old build with `cairnbook serve --db ~/cairnbook-data/cairnbook.db`, fix the cause, and try again.

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
cp -p ~/.local/bin/cairnbook ~/.local/bin/cairnbook.prev-$STAMP && \
mkdir -p ~/cairnbook-data/backups && \
cairnbook backup --db ~/cairnbook-data/cairnbook.db ~/cairnbook-data/backups/pre-update-$STAMP.db && \
ls -l ~/cairnbook-data/backups/pre-update-$STAMP.db
```

The last line is the gate: if it finds no backup file, stop. Do not install the new binary.

## 3. Install the new binary

Still in `<repo>`, from step 1:

```bash
install -m 0755 bin/cairnbook ~/.local/bin/cairnbook && cairnbook version
```

Expect the same short hash as step 1. Installing does not change the database. The first start does. Stop if any line fails.

## 4. Start the new server and check it

In the second terminal, run the server and leave it running. Ctrl-C in that terminal stops it. The first open applies pending migrations.

```bash
cairnbook serve --db ~/cairnbook-data/cairnbook.db
```

Back in the first terminal, check it:

```bash
curl -sS http://127.0.0.1:8788/healthz
cairnbook version
cd <project> && cairnbook doctor
CAIRNBOOK_API_KEY="$(sed -n 's/^CAIRNBOOK_API_KEY=//p' ~/.config/cairnbook/client.env)" <repo>/scripts/smoke-check.sh --server http://127.0.0.1:8788
```

Expect `{"status":"ok"}`, the new version, five `doctor` lines each ok, and seven `PASS` lines from the smoke check.

Restart Claude Code in the project folder so it picks up the new binary. Keep `cairnbook.prev-$STAMP` and the pre-update backup until the new server has run cleanly.

## Roll back

Use this if the new build misbehaves. It needs the same shell as step 2, so `$STAMP` is set. Stop the server first (Ctrl-C in its terminal).

If the shell was lost, set `STAMP` again. List the saved names:

```bash
ls ~/.local/bin/cairnbook.prev-* ~/cairnbook-data/backups/pre-update-*
```

The stamp is the part of the backup name between `pre-update-` and `.db`. Set it by hand, replacing the example with that stamp:

```bash
STAMP=20261111T170000Z
```

Then restore the old binary and the database:

```bash
install -m 0755 ~/.local/bin/cairnbook.prev-$STAMP ~/.local/bin/cairnbook
cairnbook version
echo ~/cairnbook-data/cairnbook.db
cairnbook restore --db ~/cairnbook-data/cairnbook.db ~/cairnbook-data/backups/pre-update-$STAMP.db
cairnbook serve --db ~/cairnbook-data/cairnbook.db
```

`cairnbook version` should print the old version. `restore` asks you to type the database path exactly as the shell expanded it, which is what `echo` printed. A different text is refused and nothing changes. It also needs a terminal: a pipe is refused. It moves the upgraded database to `~/cairnbook-data/cairnbook.db.pre-restore-<UTC time>` and never deletes it, so anything written after the upgrade is in that file. Keep it until the restored server is checked, then remove it yourself.
