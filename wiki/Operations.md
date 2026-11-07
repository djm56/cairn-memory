# Operations

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da._

This page is for whoever runs a server: keys and roles, backup, restore, logs, certificates and the common tasks. The steps for setting a server up are in [Install-Server](Install-Server). The upgrade and its rollback are in [Upgrade](Upgrade).

The commands below run on the server as root, unless they say `sudo -u cairn`. On a local install, use your own paths and drop the `sudo -u cairn`.

## Keys and roles

Every call to the server carries a key. The key's scope decides its role. The role comes from the key, never from the request.

| Scope | Role | What it can do | What it cannot do |
|---|---|---|---|
| `agent` | agent | The 15 MCP tools, and the CLI's verbs. Writes are made with it | The operator decisions, and the web UI's dashboard and browse pages. Those are refused with `operator_only` |
| `operator` | operator | Everything an agent can do. The operator decisions in the web UI: accept, reject or supersede a learning; ratify, retire or reaffirm a directive; change a project's settings | Nothing is refused to it |
| `viewer` | viewer | The seven read tools (`task_current`, `task_get`, `task_list`, `learning_list`, `directive_list`, `recall`, `search`), and every page of the web UI, including proposed learnings | Every write. A write is refused with `read_only`: as a tool error, or as exit 1 in the CLI |

Some facts about keys:

- **The format** is `cairn_` followed by 43 characters of base64url. The server makes it from 32 random bytes.
- **Only the hash is stored.** The database keeps the SHA-256 of the key. `key add` prints the key once, and it cannot be shown again. A lost key is replaced, not recovered.
- **Names are never reused.** A revoked name stays taken.
- **Last used** is when the key last made an accepted call. The server writes it at most once a minute for each key.
- **A key has one scope.** For a person who both decides and runs an agent, make two keys.

### Make, list and revoke

```bash
sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name <name> --scope agent
sudo -u cairn /usr/local/bin/cairn key list --db /var/lib/cairn/cairn.db
sudo -u cairn /usr/local/bin/cairn key revoke --db /var/lib/cairn/cairn.db --name <name>
```

**You should see** `key list` as a table: `NAME`, `SCOPE`, `CREATED`, `LAST USED` and `REVOKED`, in UTC. A key that has been used shows its last-used time. A revoked key shows the time in the last column.

`key add` writes the key on stdout, and a line on stderr saying it is shown once. Give the key to its client at once. [Install-Local](Install-Local) shows how a client stores it.

### Common key tasks

- **A new client.** Make an agent key with the client's name, and store the key on the client.
- **A new reader, such as a project manager.** Make a viewer key. It can sign in to the web UI and read every page, and it cannot change anything.
- **A rotation.** Make the new key first, switch the client to it, check that the client works, and only then revoke the old one.
- **A lost or leaked key.** Revoke it. It stops at its next call. Make a replacement for the client.
- **Do not revoke the last key of a server that listens beyond loopback.** The server refuses to start with no active key, and `Restart=on-failure` keeps retrying. Make a replacement first.

### The web UI and its sessions

The web UI is at `/ui/` on the server's address, or the proxy's address for setup (b).

- **Sign-in** takes an operator key or a viewer key, pasted once. An agent key is refused, with its own message.
- **A session** lasts at most 12 hours from sign-in, and ends after 2 hours without a request. Sign out with `/ui/signout`.
- **The cookie** is `cairn_ui`. Over TLS, or with `--behind-proxy`, it is `__Secure-cairn_ui`, which a browser keeps only for an `https` page.
- **Sign-in has no rate limit in the server.** The proxy or the firewall must rate-limit `POST /ui/signin` per client address. [DEPLOY.md](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md) explains why.

The decisions are made in the UI, not in the CLI or through MCP:

| Decision | Where |
|---|---|
| Accept, reject or supersede a proposed learning | `/ui/learnings`, or the project's learnings page |
| Ratify a learning into a directive | `/ui/directives/new` |
| Retire a directive | `/ui/directives`, on the directive |
| Reaffirm a directive's review date | `/ui/directives`, on the directive |
| Change a project's close policy or parallel-task setting | `/ui/projects/<project>/settings` |

## Backup

`cairn backup` makes a consistent copy of the database while the server runs. Each copy is new, at mode 0600, and never overwrites a file.

```bash
sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db "/var/backups/cairn/cairn-$(date -u +%Y%m%dT%H%M%SZ).db"
```

Before the first backup, make the folder, owned by the service user:

```bash
install -d -m 0700 -o cairn -g cairn /var/backups/cairn
```

What `backup` does, and what you must check:

- It copies with `VACUUM INTO` into a folder of its own beside the output, then gives the copy the output's name with a hard link. So the filesystem must support hard links. On one that does not, it fails and says so.
- It refuses an existing name, even one that points nowhere, and writes nothing through it.
- It does not migrate the database, so the copy keeps its schema.
- A killed backup can leave a hidden `.cairn-backup-*` folder beside the output, with a partial copy. Check it, then remove it by hand. `backup` does not remove it on the next run.
- **Nothing deletes old copies.** Prune them yourself, and keep enough to cover the window you need.
- **Check that a copy restores.** Restore one to a scratch path on another host, not over the live database. A backup you have never restored is not yet a backup.

[DEPLOY.md, Backup](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#backup) has the cron line and the cautions about the backup folder. Keep the folder owned by `cairn` and not writable by other users.

## Restore

`cairn restore` replaces the database, and never deletes anything. The steps are in [Upgrade](Upgrade), where it is the rollback, and in full in [DEPLOY.md, Restore](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#restore). The points that matter most:

1. **Stop the server first.** `systemctl stop cairn`. The restore can check for a running server only in some cases.
2. **Run it from an interactive shell** as the `cairn` user. Standard input must be a terminal, and you type the database path to confirm.
3. **The old database is kept** at `/var/lib/cairn/cairn.db.pre-restore-<UTC time>`. Keep it until the restored server checks out, then delete it yourself.
4. **A stray `-wal` or `-shm`** beside a path with no database refuses the restore. Move those files out by hand, then run it again.
5. **An interrupted restore** leaves the server refusing to start, rather than creating an empty database over it. The restore names the files to put back. The steps are in the README, under "If a restore fails".

## Logs

```bash
sudo journalctl -u cairn -n 20
sudo journalctl -u cairn -f
```

`journalctl` needs root, or membership of the `systemd-journal` group. Log lines are text, with UTC times. The lines you will look for:

| Line | Meaning |
|---|---|
| `msg=listening addr=... tls=... behind_proxy=... active_keys=N` | The server started. `active_keys` counts the keys active at start. A key added later is not in it. `cairn key list` shows the keys as they are now |
| `no active API key: every call will be refused with 401 until a key exists` | A loopback server started with no key |
| `msg=stopped` | The server stopped cleanly |

The server logs the method, the route and the cause of each internal failure. It never logs a header, a body or a key. A proxy's access log should not log the `Authorization` header either: [DEPLOY.md](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md) gives the proxy configurations.

## Certificates (setup a)

The server reads its certificate once, at start. When the certificate renews, restart the service so the new one is served:

```bash
sudo systemctl restart cairn
```

[DEPLOY.md, Setup (a)](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#setup-a-tls-by-files) has a `certbot` deploy hook that copies the new pair and restarts the service. A restart drops calls in flight for a moment. Clients retry.

A proxy in front (setup b) handles its own certificate. The server does not need a restart for that.

## Health checks

- **`/healthz`** answers `GET` with `{"status":"ok"}`, and needs no key. It shows no version. Use `cairn version` on the server for that.
- **`cairn key list`** shows which keys have been used, and when.
- **The operator check**, `scripts/smoke-check.sh`, runs the client-side checks against the server. [Checks](Checks) explains each line.

## Common tasks

| Task | How |
|---|---|
| Start, stop or look at the service | `sudo systemctl start cairn`, `sudo systemctl stop cairn`, `sudo systemctl status cairn` |
| Read the last log lines | `sudo journalctl -u cairn -n 20` |
| See the keys and when they were last used | `sudo -u cairn /usr/local/bin/cairn key list --db /var/lib/cairn/cairn.db` |
| Make a key for a client | `key add`, above |
| Revoke a key | `key revoke`, above |
| Back up now | `backup`, above |
| Restore a backup | [Upgrade](Upgrade) for the rollback, and [DEPLOY.md, Restore](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#restore) |
| Upgrade the binary | [Upgrade](Upgrade) |
| Pick up a renewed certificate | `sudo systemctl restart cairn` |
| Clear out old backups | By hand, after you have checked the newest one restores. Nothing does it for you |

## Local install

On a developer machine, the same commands work with your paths. There is no service manager and no `cairn` user: stop the server with Ctrl-C in its terminal, and start it again with the same `--db`. The database for a local server is a file in a folder you choose, and it is not in any project folder.
