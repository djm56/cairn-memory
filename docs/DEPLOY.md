# Deploying `cairn serve`

This is how to run `cairn serve` on a server that other machines reach. It covers
the two TLS setups of [DESIGN.md §5.3](DESIGN.md#53-the-server) (decision D20), the
systemd unit, keys, backup and restore, and upgrades. The client side is in
[the wiki's Getting Started](https://github.com/djm56/cairn-memory/wiki/Getting-Started)
and in [M2-LIVE-RUN.md](M2-LIVE-RUN.md).

Every command below runs on the server, as root or with `sudo`, unless it says
`sudo -u cairn`.

## Ports

`cairn serve` listens on `127.0.0.1:8788` by default. Loopback needs no TLS. Any other
address needs TLS files, or `--behind-proxy` when a proxy in front terminates TLS
(D20). A non-loopback address with neither is refused at start, and so is one with no
active API key.

## Install the binary

Both setups need `cairn` at `/usr/local/bin/cairn` before anything else, because the
key and the unit both run it. From a checkout, `make build` writes `bin/cairn`:

```bash
install -m 0755 bin/cairn /usr/local/bin/cairn
```

## Choose a setup

- **(a) TLS by files.** `cairn serve` listens on a non-loopback address and reads a
  certificate and key. Use it when no proxy sits in front.
- **(b) Behind a reverse proxy.** `cairn serve` stays on loopback, and a proxy such as
  Caddy or nginx terminates TLS and forwards to it. Use it when a proxy is already
  there. The default unit below is set up for (b).

Each setup has its own numbered order below. Follow it from the top on a fresh host.
The order matters, because the server refuses to start in some states (see
[Why the order matters](#why-the-order-matters)):

- The server needs at least one active API key before it accepts calls (see
  [Keys](#keys)). On a **non-loopback** address it refuses to start without one.
- A key can only be made once the service user and its state directory exist, because
  `key add` creates the database file, not the directory it goes in.

### Setup (a): TLS by files

Run these in this order, on a fresh host. Each step needs the ones before it.

1. **Create the service user.** It owns the database and runs the service:

   ```bash
   useradd --system --no-create-home --home-dir /var/lib/cairn --shell /usr/sbin/nologin cairn
   ```

2. **Create the state directory**, owner-only, owned by that user:

   ```bash
   install -d -m 0700 -o cairn -g cairn /var/lib/cairn
   ```

3. **Make the first key**, as the service user. The server needs it to start on a
   non-loopback address. `key add` creates the database file and prints the key once;
   copy it now (see [Keys](#keys)):

   ```bash
   sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name ops --scope operator
   ```

4. **Install the certificate and key**, where the service user can read them and
   nobody else can. The group `cairn` exists after step 1. Use the files your CA gave
   you (a Let's Encrypt pair is under `/etc/letsencrypt/live/<name>/`):

   ```bash
   install -d -m 0750 -o root -g cairn /etc/cairn/tls
   install -m 0640 -o root -g cairn fullchain.pem /etc/cairn/tls/cert.pem
   install -m 0640 -o root -g cairn privkey.pem /etc/cairn/tls/key.pem
   ```

5. **Install the unit, then the setup (a) override.** The unit is in
   [The systemd unit](#the-systemd-unit). The override replaces `ExecStart` with the
   address your clients reach and the TLS files. `systemctl edit` opens an editor and
   reloads systemd when you save:

   ```bash
   install -m 0644 docs/deploy/cairn.service /etc/systemd/system/cairn.service
   systemctl edit cairn
   ```

   ```ini
   [Service]
   ExecStart=
   ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr 0.0.0.0:8788 --tls-cert /etc/cairn/tls/cert.pem --tls-key /etc/cairn/tls/key.pem
   ```

   Use a specific LAN address in place of `0.0.0.0` if the host has more than one.

6. **Start it:**

   ```bash
   systemctl daemon-reload
   systemctl enable --now cairn
   systemctl status cairn
   ```

   The server's log says `tls=true`. Open the port in the firewall to the clients only.

**Use a high port.** The unit's `CapabilityBoundingSet=` is empty, so the service
cannot bind a port below 1024: `:443` fails with "permission denied". Port 8788, or
any port above 1023, is the supported choice. To serve on 443, use setup (b) with a
proxy.

**Certificate renewal needs a restart.** `cairn serve` reads the certificate and key
once, at start. A certificate renewed on disk is not served until the server restarts,
and clients then fail verification when the old one expires. With Let's Encrypt's
`certbot`, a deploy hook copies the renewed pair and restarts the service. Put it in
`/etc/letsencrypt/renewal-hooks/deploy/cairn.sh`, mode 0755. `certbot` runs it only
after a successful renewal, and sets `RENEWED_LINEAGE`:

```sh
#!/bin/sh
set -eu
install -m 0640 -o root -g cairn "$RENEWED_LINEAGE/fullchain.pem" /etc/cairn/tls/cert.pem
install -m 0640 -o root -g cairn "$RENEWED_LINEAGE/privkey.pem" /etc/cairn/tls/key.pem
systemctl restart cairn
```

The restart drops calls in flight for a moment; clients retry. With another CA, do the
same: copy the new files in, then `systemctl restart cairn`.

### Setup (b): behind a reverse proxy

The unit listens on `127.0.0.1:8788`, so only processes on this host reach it. The
proxy terminates TLS and forwards to that address. Run these in this order, on a
fresh host:

1. **Create the service user:**

   ```bash
   useradd --system --no-create-home --home-dir /var/lib/cairn --shell /usr/sbin/nologin cairn
   ```

2. **Create the state directory** before any `key add`, which creates the database
   file and not its directory:

   ```bash
   install -d -m 0700 -o cairn -g cairn /var/lib/cairn
   ```

3. **Install the unit and start it.** On loopback the server may start with no key; it
   prints `no active API key` and refuses every call with 401 until one exists:

   ```bash
   install -m 0644 docs/deploy/cairn.service /etc/systemd/system/cairn.service
   systemctl daemon-reload
   systemctl enable --now cairn
   systemctl status cairn
   ```

4. **Make the first key**, as the service user (see [Keys](#keys)):

   ```bash
   sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name ops --scope operator
   ```

5. **Configure the proxy**, as below, and reload it.

If the proxy is **not on this host**, the server must listen on a non-loopback address
with `--behind-proxy`, and then it needs a key before it starts. Use this order
instead: steps 1 and 2; then step 4 (the key); then install the unit and this
override (`systemctl edit cairn`), and start it. The `<address>` is the LAN address the
proxy reaches:

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr <address>:8788 --behind-proxy
```

The server then warns on each start that the port must be reachable only from the
proxy. Firewall it to the proxy's address.

Two rules for the proxy:

- **It must not log the `Authorization` header.** That header carries the API key.
  Do not add `$http_authorization` or any header dump to a log format.
- **It must cap the body at 1 MiB** (1048576 bytes), or pass the body through. The
  server caps each call at 1 MiB and refuses more; the proxy is the place to cap
  earlier.

Caddy, with TLS handled by Caddy:

```caddy
cairn.example.net {
	reverse_proxy 127.0.0.1:8788
	request_body {
		max_size 1048576
	}
	log {
		output file /var/log/caddy/cairn.log
		format filter {
			wrap json
			fields {
				request>headers>Authorization delete
			}
		}
	}
}
```

nginx, with a certificate you supply:

```nginx
server {
    listen 443 ssl;
    server_name cairn.example.net;
    ssl_certificate     /etc/ssl/cairn/fullchain.pem;
    ssl_certificate_key /etc/ssl/cairn/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    client_max_body_size 1m;                       # 1 MiB
    access_log /var/log/nginx/cairn.access.log;    # the default format logs no Authorization

    location / {
        proxy_pass http://127.0.0.1:8788;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

With a local proxy, the server logs `tls=false`. That is expected. Caddy's automatic
certificates need a public DNS name and open ports 80 and 443; for a server without
one, see [A server with no public DNS name](#a-server-with-no-public-dns-name).

### Why the order matters

These are the server's own refusals. The documents follow them; they are not
settings to work around.

| If you do this first | The server or the command says | Because |
|---|---|---|
| Start a non-loopback server with no key | `the address is not loopback, so it needs at least one active API key` | A network-reachable server with no key would have nothing to check calls against (`RequireKeys`). `Restart=on-failure` then loops on it. |
| Start a non-loopback server with no TLS files and no `--behind-proxy` | `the address is not loopback, so it needs TLS` | Keys would cross the network in the clear (`RequireTransport`). |
| `key add` before `/var/lib/cairn` exists | an error ending `open <directory>/: no such file or directory` | `key add` creates the database file, not its directory. |
| `install ... -g cairn` before the user exists | `install: invalid group 'cairn'` | The group is made with the user. |

A loopback server may start with no key, which is why setup (b) with a local proxy
can start first.

## A server with no public DNS name

Caddy's automatic TLS needs a public name; a home or office server often has none.
`cairn` has no flag to skip certificate checks, and no flag for a private CA. It
trusts the operating system's certificate roots, and nothing else. Two routes work.

**An SSH tunnel.** The server stays on loopback (the default unit, setup (b) steps 1
to 4, with no proxy), and the client reaches it through SSH. The tunnel encrypts the
traffic, and the client sees a loopback address, so no certificate is needed. On the
client, keep it running while you work:

```bash
ssh -N -o ExitOnForwardFailure=yes -L 8788:127.0.0.1:8788 you@server
```

Set `server = "http://127.0.0.1:8788"` in `.cairn.toml`. Plain `http` to a loopback
address is not warned about by `cairn doctor`: it warns only for a host that is not
loopback. The tunnel must be up whenever Claude Code runs. If port 8788 is taken on the
client, use another local port, such as `-L 18788:127.0.0.1:8788`, and put the same port
in `.cairn.toml`.

**A private CA the client trusts.** Make a CA, issue the server a certificate whose
subject alternative name is the name or address clients use, use it in setup (a) or in
the proxy, and install the CA certificate in each client's operating system trust store.
On macOS, `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ca.pem`.
On Debian or Ubuntu, copy it to `/usr/local/share/ca-certificates/cairn-ca.crt` and run
`sudo update-ca-certificates`. Then use `https://` in `.cairn.toml`. A client that does
not trust the CA fails the connection, and that is the correct result: do not look for
a way round it. These commands are given from the platforms' documentation and have not
been run for this project.

## The service user and state directory

The service user owns the database and runs the service. The state directory,
`/var/lib/cairn`, is mode 0700 and owned by that user, so nothing else on the host reads
the database. The server creates the database there at mode 0600. The commands are in
the numbered orders above. The unit's `StateDirectory=cairn` makes the directory too,
but only once the unit starts, which is too late for the first `key add` and too late
for a non-loopback server that needs that key to start. That is why the directory is
made by hand first.

## The systemd unit

The unit is installed in the numbered orders above. It is
[docs/deploy/cairn.service](deploy/cairn.service):

```ini
[Unit]
Description=Cairn memory server
Documentation=https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=cairn
Group=cairn
ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr 127.0.0.1:8788
Restart=on-failure
RestartSec=5

StateDirectory=cairn
StateDirectoryMode=0700
WorkingDirectory=/var/lib/cairn
UMask=0077

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/cairn
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

**This unit is unverified until the live run.** It was checked by reading the
directives against what the server does, and it has not been started under systemd or
run through `systemd-analyze`. Its first start is the M2 live run
([M2-LIVE-RUN.md](M2-LIVE-RUN.md), step 1.2). Check it on your host:

```bash
systemd-analyze verify /etc/systemd/system/cairn.service
systemctl status cairn
systemd-analyze security cairn
```

`ReadWritePaths=` repeats what `StateDirectory=` already grants under
`ProtectSystem=strict`. It is kept on purpose, so the unit still works if
`StateDirectory=` is removed.

The unit holds no secrets. Keys are not in it, and clients send them. The state
directory is the only path the service may write. `Restart=on-failure` restarts a
crash, and a clean stop stays stopped.

Logs go to the journal: `journalctl -u cairn`, as root or as a member of the
`systemd-journal` group. The server logs the method, the route path and the cause of
each internal failure. It never logs a header, a body or a key.

## Keys

Keys are made in the server's database, as the service user, so the file stays owned by
`cairn`. Give `--db` the same path the unit uses. The first key, `ops`, is made in the
numbered order of your setup, before the server starts; a name is never reused, so
later keys take other names. `key add` needs the state directory to exist already (it
creates the database file, not the directory):

```bash
sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name laptop-claude --scope agent
sudo -u cairn /usr/local/bin/cairn key list --db /var/lib/cairn/cairn.db
```

`--scope` is `operator` for decisions and ratification, `agent` for the MCP tools, or
`viewer` for read-only access. `key add` prints the key once, on stdout. Copy it then,
to the place the client needs it. It is shown once and never again: the database keeps
only a hash. A key that was lost is revoked and replaced:

```bash
sudo -u cairn /usr/local/bin/cairn key revoke --db /var/lib/cairn/cairn.db --name laptop-claude
```

A key looks like `cairn_<your-key>`. Never paste a real key into a file that is
committed, a ticket, or a chat.

## Backup

`cairn backup` makes a consistent copy while the server runs. Schedule it with cron, as
the service user. Write the backups to a directory only that user can read:

```bash
install -d -m 0700 -o cairn -g cairn /var/backups/cairn
```

`/etc/cron.d/cairn-backup`:

```cron
30 2 * * * cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db "/var/backups/cairn/cairn-$(date -u +\%Y\%m\%dT\%H\%M\%SZ).db"
```

That directory being 0700 and owned by `cairn` is what closes the symlink race of #129
in practice: only that user can plant a link in it. Do not point backups at a shared
directory such as `/tmp`, where another user could.

Each copy is new and owner-only, and `backup` refuses an existing name, so a run
never overwrites one. Nothing deletes old copies. Prune them yourself, and keep enough
to cover the window you need. Check that a copy restores: restore one to a scratch path
on another host, not over the live database.

## Restore

`cairn restore` replaces the database. It is confirmed at a terminal, by typing the
database path, and it never deletes anything: the old database is moved aside.

1. Stop the server. The restore checks for a running server on a readable database,
   but a file it cannot read is not checked, so stop it yourself first:

   ```bash
   systemctl stop cairn
   ```

2. Run the restore from an interactive shell as the service user:

   ```bash
   sudo -u cairn /usr/local/bin/cairn restore --db /var/lib/cairn/cairn.db /var/backups/cairn/cairn-20261031T023000Z.db
   ```

3. Start the server again, and check the log:

   ```bash
   systemctl start cairn
   journalctl -u cairn -n 20
   ```

The old database stays at `/var/lib/cairn/cairn.db.pre-restore-<UTC time>`. Keep it
until the restored server checks out, then delete it yourself.

**If a restore fails.** A restore that stops after its first move says where the
database before it is, and the commands that put it back. In short: stop `cairn`, move
any file now at `/var/lib/cairn/cairn.db` aside, then run the `mv` command the error
printed. The server also refuses to create an empty database over an interrupted
restore, and names the file. The full steps are in the README under "If a restore
fails".

## Upgrade

Upgrade the server before its clients ([DESIGN.md §5.4](DESIGN.md#54-the-http-wire-form)).
Migrations are one-way: a new binary changes the schema when it starts, and an old binary
cannot read it back. So keep the old binary and take the backup first; both are needed
to go back.

```bash
systemctl stop cairn
cp -p /usr/local/bin/cairn /usr/local/bin/cairn.prev
sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db "/var/backups/cairn/pre-upgrade-$(date -u +%Y%m%dT%H%M%SZ).db"
install -m 0755 bin/cairn /usr/local/bin/cairn
systemctl start cairn
journalctl -u cairn -n 20
```

Migrations run when the server starts, so the first start after an upgrade may change the
schema. Keep `cairn.prev` and the pre-upgrade backup until the new server has run
cleanly.

**Roll back.** Restoring the old binary alone is not enough: the old binary does not
understand the new schema. Put back the old binary **and** the pre-upgrade backup:

```bash
systemctl stop cairn
install -m 0755 /usr/local/bin/cairn.prev /usr/local/bin/cairn
sudo -u cairn /usr/local/bin/cairn restore --db /var/lib/cairn/cairn.db /var/backups/cairn/pre-upgrade-<UTC time>.db
systemctl start cairn
```

Anything written after the upgrade started is in the database that `restore` moves aside
(`cairn.db.pre-restore-<UTC time>`), not in the restored one. See [Restore](#restore).
