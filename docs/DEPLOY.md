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
(D20). A non-loopback address with neither is refused at start.

## Choose a setup

- **(a) TLS by files.** `cairn serve` listens on a non-loopback address and reads a
  certificate and key. Use it when no proxy sits in front.
- **(b) Behind a reverse proxy.** `cairn serve` stays on loopback, and a proxy such as
  Caddy or nginx terminates TLS and forwards to it. Use it when a proxy is already
  there. The default unit below is set up for (b).

Whichever you choose, the server needs at least one active API key before it accepts
calls (see [Keys](#keys)).

### Setup (a): TLS by files

Put the certificate and key where the service user can read them, and nobody else:

```bash
install -d -m 0750 -o root -g cairn /etc/cairn/tls
install -m 0640 -o root -g cairn fullchain.pem /etc/cairn/tls/cert.pem
install -m 0640 -o root -g cairn privkey.pem /etc/cairn/tls/key.pem
```

Then start the server on the address your clients reach, with the files. Override the
unit's `ExecStart` with `systemctl edit cairn`:

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr 0.0.0.0:8788 --tls-cert /etc/cairn/tls/cert.pem --tls-key /etc/cairn/tls/key.pem
```

Use a specific LAN address in place of `0.0.0.0` if the host has more than one. Open
the port in the firewall to the clients only. The server's log says `tls=true`.

### Setup (b): behind a reverse proxy

The unit listens on `127.0.0.1:8788`, so only processes on this host reach it. The
proxy terminates TLS and forwards to that address. Two rules for the proxy:

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

With a proxy, the server logs `tls=false`. That is expected. A proxy that is not on
this host needs `--behind-proxy` and a non-loopback `--addr`; that setup is not the
default, and the server then warns on each start that the port must be reachable only
from the proxy.

## Users and the state directory

Create a dedicated system user. It owns the database and runs the service:

```bash
useradd --system --home-dir /var/lib/cairn --shell /usr/sbin/nologin cairn
```

The unit creates `/var/lib/cairn` at mode 0700, owned by `cairn`. The server creates the
database there at mode 0600. Nothing else on the host needs to read it.

## The systemd unit

Build or install the binary, then install the unit:

```bash
install -m 0755 bin/cairn /usr/local/bin/cairn
install -m 0644 docs/deploy/cairn.service /etc/systemd/system/cairn.service
systemctl daemon-reload
systemctl enable --now cairn
```

The unit, from [docs/deploy/cairn.service](deploy/cairn.service):

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

The unit holds no secrets. Keys are not in it, and clients send them. The state
directory is the only path the service may write. `Restart=on-failure` restarts a
crash, and a clean stop stays stopped.

Logs go to the journal: `journalctl -u cairn`. The server logs the method, the route
path and the cause of each internal failure. It never logs a header, a body or a key.

## Keys

Keys are made in the server's database, as the service user, so the file stays owned by
`cairn`. Give `--db` the same path the unit uses:

```bash
sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name ops --scope operator
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

```bash
systemctl stop cairn
sudo -u cairn /usr/local/bin/cairn backup --db /var/lib/cairn/cairn.db "/var/backups/cairn/pre-upgrade-$(date -u +%Y%m%dT%H%M%SZ).db"
install -m 0755 bin/cairn /usr/local/bin/cairn
systemctl start cairn
journalctl -u cairn -n 20
```

Migrations run when the server starts, so the first start after an upgrade may change the
schema. Keep the pre-upgrade backup until the new server has run cleanly.
