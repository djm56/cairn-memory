# Install a server

_Status as of 2026-11-06: M5 (Plugin) is current. M2's live run (#19) passed on 2026-11-02 on Ubuntu 24.04 with the systemd unit. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the code at 8b651da._

This page sets up `cairn serve` on a Linux server that other machines reach, as a systemd service. It is followable from top to bottom. Where a file's full text is long, such as the systemd unit or a proxy configuration, the page links to its section in [DEPLOY.md](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md) and says what to do with it. The commands on this page are the ones DEPLOY.md gives, in the same order.

If you only want Cairn on one developer machine, read [Install-Local](Install-Local) instead.

## Quick checklist

- [ ] A Linux server with systemd, root or sudo access, and SSH to it. The live run used Ubuntu 24.04. Other distributions are not tested.
- [ ] A `cairn` binary for the server's CPU: `amd64` by default, or `arm64` (step 1).
- [ ] The binary at `/usr/local/bin/cairn`.
- [ ] A service user `cairn`, a state directory `/var/lib/cairn` and a backup folder `/var/backups/cairn`, both mode 0700 and owned by `cairn`.
- [ ] A first operator key, made as the `cairn` user.
- [ ] Setup (a), TLS by certificate files, or setup (b), behind a reverse proxy. Pick one.
- [ ] The systemd unit installed, with the override for your setup, and the service running.
- [ ] A firewall rule that lets clients reach the port, and nothing else. The docs do not name a firewall tool.
- [ ] `curl` to `/healthz` answers `{"status":"ok"}` from another machine.
- [ ] A backup scheduled, and one restore tried on a scratch path.

## Before you start

**The binary.** Build it on the server, or build it on another machine and copy it. Part 1 does both. In either case the server ends up with one file at `/usr/local/bin/cairn`.

**The two setups.** Choose one before you start. It decides how the server listens.

- **Setup (a): TLS by files.** `cairn serve` listens on a non-loopback address, and reads a certificate and a private key. Use it when nothing sits in front of the server.
- **Setup (b): behind a proxy.** `cairn serve` stays on `127.0.0.1:8788`, and a proxy such as Caddy, nginx or Apache terminates TLS and forwards to it. Use it when a proxy is already there. The default unit is set up for this.

Each setup has its own order of steps, because the server refuses to start in some states. [DEPLOY.md, Why the order matters](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#why-the-order-matters) lists those refusals.

**Ports.** The server listens on `127.0.0.1:8788` unless `--addr` says otherwise. Port 8788 is the default, not a requirement. Any port above 1023 works, because the unit cannot bind a port below 1024.

## Part 1: The binary

Both setups need the binary at `/usr/local/bin/cairn` before anything else, because the key and the unit both run it.

### Option A: build on the server

On a checkout of the repository on the server, with Go 1.26 or later:

```bash
cd cairn-memory
make build
sudo install -m 0755 bin/cairn /usr/local/bin/cairn
```

`make build` writes `bin/cairn` in the checkout.

### Option B: build on another machine and copy it

On your machine, from the checkout:

```bash
make push SERVER=<user>@<host>
```

Add `SSH_PORT=<port>` when SSH is not on port 22. `make push` does three things, in this order:

1. Builds `bin/cairn-linux` for Linux on `amd64`. On a server whose `uname -m` prints `aarch64`, add `LINUX_ARCH=arm64`.
2. Copies it with `scp` to `cairn-linux` in the **home directory of the SSH user**: `/home/you/cairn-linux` for the user `you`. When you connect as root, the file is in root's home directory, which a root shell reaches as `~`.
3. Runs `./cairn-linux version` on the server, from that home directory.

It installs nothing. On the server, install the file, using its full path:

```bash
sudo install -m 0755 /home/you/cairn-linux /usr/local/bin/cairn
```

Use the full path, not `~/cairn-linux`. In a root shell, such as one from `sudo -s`, `~` is `/root`, and the file is not there. `make push` copies over the old `~/cairn-linux` each time, so keep the previous binary under `/usr/local/bin`, as [Upgrade](Upgrade) does.

**You should see** on the server:

```bash
/usr/local/bin/cairn version
```

which prints `cairn <hash>`. A binary made with a plain `go build` prints `cairn dev`; use `make build` or `make build-linux`, which stamp the hash. [Binaries](Binaries) explains the version strings.

## Part 2: The service user, the state directory and the backup folder

Run these on the server, as root or with sudo. The `cairn` user owns the database and runs the service:

```bash
sudo useradd --system --no-create-home --home-dir /var/lib/cairn --shell /usr/sbin/nologin cairn
sudo install -d -m 0700 -o cairn -g cairn /var/lib/cairn
sudo install -d -m 0700 -o cairn -g cairn /var/backups/cairn
```

`useradd` also makes the group `cairn`. The state directory is owner-only, so no other account on the host reads the database.

`key add` creates the database **file**, not the directory, so the directory must exist first.

The backup folder is made here, on a fresh install, so the first backup and the first upgrade have somewhere to write. Its mode is 0700 and it is owned by `cairn`, the same as [DEPLOY.md, Backup](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#backup) gives. [Upgrade](Upgrade) relies on it.

## Part 3a: Setup (a), TLS by files

Run these in this order on a fresh host. Parts 1 and 2 come first.

### Step 1: The first key

The server needs at least one active key before it starts on a non-loopback address. Make the first key as the service user. The command creates the database file, `/var/lib/cairn/cairn.db`, at mode 0600, and prints the key once:

```bash
sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name ops --scope operator
```

**You should see** the key on stdout, one line starting `cairn_`, and on stderr `cairn key add: the key is shown once above`. Copy it now. The key is what you use to sign in to the web UI and to make decisions. Agents get their own keys later, see [Operations](Operations).

### Step 2: The certificate and its key

Install the certificate and key where the service user can read them, and nobody else can. Use the files your certificate authority gave you. A Let's Encrypt pair is under `/etc/letsencrypt/live/<name>/`:

```bash
sudo install -d -m 0750 -o root -g cairn /etc/cairn/tls
sudo install -m 0640 -o root -g cairn fullchain.pem /etc/cairn/tls/cert.pem
sudo install -m 0640 -o root -g cairn privkey.pem /etc/cairn/tls/key.pem
```

The server reads the certificate once, at start. When a certificate renews, the server must restart before it serves the new one. [DEPLOY.md, Certificate renewal](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#setup-a-tls-by-files) has a renewal hook for `certbot`.

### Step 3: The unit, with the setup (a) override

Install the unit file. Its full text is in [docs/deploy/cairn.service](https://github.com/djm56/cairn-memory/blob/main/docs/deploy/cairn.service), and [DEPLOY.md, The systemd unit](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#the-systemd-unit) shows it. The server may have no checkout, so copy the file to the server first, from the checkout on your machine:

```bash
scp docs/deploy/cairn.service <user>@<host>:
```

Then, on the server, install it from the home directory the copy went to:

```bash
sudo install -m 0644 /home/you/cairn.service /etc/systemd/system/cairn.service
```

When you connect as root, the copy is in root's home directory, so use `~/cairn.service` from a root shell. If the commands run from a checkout on the server, `docs/deploy/cairn.service` in that checkout works in place of the copy.

Then add the override. `systemctl edit` opens an editor, and reloads systemd when you save. The override replaces `ExecStart` with the address clients reach, and the certificate files:

```bash
sudo systemctl edit cairn
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr 0.0.0.0:8788 --tls-cert /etc/cairn/tls/cert.pem --tls-key /etc/cairn/tls/key.pem
```

Use a specific LAN address in place of `0.0.0.0` when the host has more than one. The empty `ExecStart=` line is needed: it clears the one from the unit.

### Step 4: Start it

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now cairn
sudo systemctl status cairn
```

**You should see** `active (running)`. The log line in Part 5, step 2 should show `tls=true`.

### Step 5: The firewall

Open the port to the clients only, not to the whole internet. The docs do not name a firewall tool; use the one the host has. Remember that the server's port is the one in the `--addr` override.

## Part 3b: Setup (b), behind a reverse proxy

Run these in this order on a fresh host. Parts 1 and 2 come first.

### Step 1: The unit, on loopback

The shipped unit listens on `127.0.0.1:8788`, so only processes on this host reach the server. Install it and start it. A loopback server may start with no key. It prints `no active API key` and refuses every call with 401 until a key exists. Copy the file to the server first, as Part 3a, step 3 shows (`scp docs/deploy/cairn.service <user>@<host>:` from your checkout), then:

```bash
sudo install -m 0644 /home/you/cairn.service /etc/systemd/system/cairn.service
sudo systemctl daemon-reload
sudo systemctl enable --now cairn
sudo systemctl status cairn
```

### Step 2: The first key

```bash
sudo -u cairn /usr/local/bin/cairn key add --db /var/lib/cairn/cairn.db --name ops --scope operator
```

**You should see** the key once, as in setup (a), step 1.

### Step 3: The proxy

Configure the proxy to terminate TLS and forward to `http://127.0.0.1:8788`, then reload it. [DEPLOY.md, Setup (b)](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#setup-b-behind-a-reverse-proxy) has working Caddy, nginx and Apache configurations. Each one:

- keeps the `Host` header unchanged. The web UI refuses a form whose `Origin` does not match `Host`, so a rewritten `Host` makes every sign-in fail with "The form could not be verified."
- caps the request body at 1 MiB.
- does not log the `Authorization` header, which carries the key.

Rate-limit `POST /ui/signin` at the proxy or the firewall, per client address. The server has no rate limit on sign-in of its own.

### Step 4: The `--behind-proxy` override

Without `--behind-proxy` the web UI refuses every form, because its origin check expects `http://` while the browser sends `https://`. The shipped unit does not pass the flag. Add the override:

```bash
sudo systemctl edit cairn
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/cairn serve --db /var/lib/cairn/cairn.db --addr 127.0.0.1:8788 --behind-proxy
```

The same override ships as [docs/deploy/cairn-behind-proxy.conf](https://github.com/djm56/cairn-memory/blob/main/docs/deploy/cairn-behind-proxy.conf), a systemd drop-in. Use the file for this setup only. The flag makes the session cookie `Secure`, so the browser keeps it only on an `https` page. To use the file instead of the inline override, copy it to the server from your checkout, then install it as a drop-in:

```bash
scp docs/deploy/cairn-behind-proxy.conf <user>@<host>:
sudo install -d -m 0755 /etc/systemd/system/cairn.service.d
sudo install -m 0644 /home/you/cairn-behind-proxy.conf /etc/systemd/system/cairn.service.d/cairn-behind-proxy.conf
```

Use one or the other, not both. Then restart. Use `restart`, not `start`: `start` does nothing to a unit that is already running:

```bash
sudo systemctl daemon-reload
sudo systemctl restart cairn
```

**You should see** `behind_proxy=true` in the log line in Part 5, step 2.

### A proxy on another host

When the proxy is not on this server, the server must listen on a non-loopback address with `--behind-proxy`. That needs a key before it starts, so the order changes: make the state directory, then the key, then install the unit and the override with `--addr <address>:8788` (the LAN address the proxy reaches), then `daemon-reload` and `restart`. The override and the order are in [DEPLOY.md, Setup (b)](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#setup-b-behind-a-reverse-proxy). The server then warns on each start that its port must be reachable only from the proxy, so firewall the port to the proxy's address.

### Step 5: The firewall

For setup (b) on this host, the port stays on loopback, and the public port is the proxy's (443). If the proxy is on another host, allow port 8788 from that host only.

## Part 4: Ports, addresses and the refusals

The server checks its settings in a fixed order, and refuses to start when they are wrong. These are the refusals you are most likely to see, with the text the server prints:

| Setting | What the server refuses | The message |
|---|---|---|
| `--addr` is not loopback, and there are no TLS files and no `--behind-proxy` | Start | `the address is not loopback, so it needs TLS: give --tls-cert and --tls-key, or --behind-proxy if a proxy in front terminates TLS` |
| `--addr` is not loopback, and there is no active key | Start | `the address is not loopback, so it needs at least one active API key (DESIGN §8)` |
| `--tls-cert` without `--tls-key`, or the reverse | Start (usage error, exit 2) | `--tls-cert and --tls-key go together: give both, or neither` |
| A loopback address and no key | Starts, with a warning | `no active API key: every call will be refused with 401 until a key exists` |

The bind rules, and the reasoning, are in [DEPLOY.md, Why the order matters](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#why-the-order-matters).

## Part 5: Verify

Run these after the server starts.

1. **The service is running.**

   ```bash
   sudo systemctl status cairn
   ```

   **You should see** `active (running)`.

2. **The log names the setup.** The listening line shows the address, `tls=`, `behind_proxy=` and `active_keys=`:

   ```bash
   sudo journalctl -u cairn -n 20
   ```

   Setup (a) shows `tls=true`. Setup (b) shows `behind_proxy=true`. `active_keys` counts the keys active when the server started, so it can read `0` after a key was added while it ran. `cairn key list` shows the keys as they are now.

3. **`/healthz` answers from another machine.** Use the `https://` address for setup (a) or the proxy's address for setup (b):

   ```bash
   curl -sS https://<host>:8788/healthz
   ```

   **You should see** `{"status":"ok"}`. Do not use `curl -k`. A certificate error is a fault to fix, not to skip. `/healthz` shows no version. Use `cairn version` on the server for that.

4. **A client reaches the server.** On a client with a key, from the project folder, run `cairn doctor`. Its `reach` line should read `ok`. [Checks](Checks) explains the operator check.

5. **The web UI signs in.** Open `https://<host>/ui/` in a browser, or the proxy's address, and sign in with the operator key from Part 3. A viewer key can read every page but change nothing. An agent key is refused at sign-in.

## Part 6: After the install

- **Agent keys.** Make one key per client, with `--scope agent`. [Operations](Operations) covers keys, roles and rotation.
- **Backups.** Schedule `cairn backup` and check that a copy restores. The folder `/var/backups/cairn` was made in Part 2. DEPLOY.md has the cron line and the cautions: [Backup](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#backup) and [Restore](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md#restore). [Operations](Operations) summarises them.
- **Upgrades.** Read [Upgrade](Upgrade) before you replace the binary. A newer build changes the database when it starts, and the older build cannot open that database again.
- **Logs.** `journalctl -u cairn`. The server never logs a header, a body or a key.

If a step does not match what you see, read [Troubleshooting](Troubleshooting).
