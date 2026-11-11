# Quick update: a test server to the latest main

_Status as of 2026-11-11, main 18fc813. Each command was checked against the source or `cairn --help`. None was run end to end when this was written._

This is the fast path for an install made with [QUICK-INSTALL.md](QUICK-INSTALL.md). The wiki is the full reference: [Upgrade](../wiki/Upgrade.md) explains each step and the schema rules. Upgrade the server before its clients.

**Warning.** Do not run any `cairn key` command with the new binary until you are ready to upgrade. `key list` also opens the database and migrates it. The first `cairn serve` on the new build migrates it too.

## 1. Note the version and pull main

```bash
cairn version                              # the old version
cd <repo>
git switch main && git pull --ff-only
```

## 2. Stop the server and back up with the old binary

Press Ctrl-C in the server terminal first. Then, in the first terminal:

```bash
STAMP=$(date -u +%Y%m%dT%H%M%SZ)           # keep this shell: the rollback uses STAMP
cp -p ~/.local/bin/cairn ~/.local/bin/cairn.prev-$STAMP
mkdir -p ~/cairn-data/backups
cairn backup --db ~/cairn-data/cairn.db ~/cairn-data/backups/pre-update-$STAMP.db
ls -l ~/cairn-data/backups/pre-update-$STAMP.db   # gate: if the file is missing, stop
```

If `ls` finds no file, stop. The database is unchanged. Start the old build with `cairn serve --db ~/cairn-data/cairn.db` and fix the cause before you go on.

## 3. Build and install the new binary

```bash
cd <repo>
make build && bin/cairn version            # expect the new short sha
install -m 0755 bin/cairn ~/.local/bin/cairn
```

Installing does not change the database. The first start does.

## 4. Start the new server and check it

```bash
cairn serve --db ~/cairn-data/cairn.db     # second terminal; the first open applies pending migrations
curl -sS http://127.0.0.1:8788/healthz     # {"status":"ok"}
cairn version
cd <project> && cairn doctor               # five lines, each ok
CAIRN_API_KEY="$(sed -n 's/^CAIRN_API_KEY=//p' ~/.config/cairn/client.env)" <repo>/scripts/smoke-check.sh --server http://127.0.0.1:8788   # expect 7 PASS
```

Keep `cairn.prev-$STAMP` and the pre-update backup until the new server has run cleanly.

## Roll back

Use this if the new build misbehaves. It needs the same shell as step 2, so `$STAMP` is set. If the shell is lost, `ls ~/.local/bin/cairn.prev-* ~/cairn-data/backups/pre-update-*` lists the names.

```bash
# stop the server (Ctrl-C)
install -m 0755 ~/.local/bin/cairn.prev-$STAMP ~/.local/bin/cairn
cairn version                              # the old version
echo ~/cairn-data/cairn.db                 # the path restore will ask you to type, expanded
cairn restore --db ~/cairn-data/cairn.db ~/cairn-data/backups/pre-update-$STAMP.db
cairn serve --db ~/cairn-data/cairn.db
```

`restore` asks you to type the database path exactly as the shell expanded it, which is what `echo` printed. A different text is refused and nothing changes. It also needs a terminal: a pipe is refused. It moves the upgraded database to `~/cairn-data/cairn.db.pre-restore-<UTC time>` and never deletes it, so anything written after the upgrade is in that file. Keep it until the restored server is checked, then remove it yourself.
