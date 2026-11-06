# Binaries

_Status as of 2026-11-06: M5 (Plugin) is current. M4's exit check passed by operator ruling; sections 4 and 5 of the [hand-check guide](https://github.com/djm56/cairn-memory/blob/main/docs/M4-HAND-CHECK.md) are not run. Checked against the Makefile and the code at 8b651da._

There is one binary, `cairn`. It is the MCP server, the server, the hook helper and the command-line tool. This page covers how to build it, what the version string means, which systems it is built for, and where to put it. The Makefile has 12 targets. This page lists all 12.

Cairn ships no release archives yet. Until it does, you build from source. The release work is Later in the plan ([PLAN.md §3](https://github.com/djm56/cairn-memory/blob/main/docs/PLAN.md#3-milestones)).

## The Makefile targets

Run `make <target>` from the root of the repository. `make` with no target runs `build`, which is the first target in the Makefile.

| Target | What it runs | Output |
|---|---|---|
| `build` | `go build` with the version stamped in, for this machine | `bin/cairn` |
| `build-linux` | The same build for Linux, with `CGO_ENABLED=0`, for `GOARCH` set by `LINUX_ARCH` (`amd64` by default) | `bin/cairn-linux` |
| `push` | `build-linux`, then `scp` to the home directory of `SERVER`, then runs `version` there. It installs nothing | `bin/cairn-linux`, and `cairn-linux` in the remote home directory |
| `test` | `go test -race ./...` | none |
| `vet` | `go vet ./...` | none |
| `fmt` | `gofmt -w .`, which rewrites files in place | none |
| `fmt-check` | `gofmt -l .`, and fails if any file is listed | none |
| `cross` | `go build` and `go vet` for Windows, macOS, FreeBSD and OpenBSD. A compile check: nothing runs | none |
| `scan-invisible` | `git grep` for bidirectional, invisible and tag characters, and a byte-order mark | none |
| `scan-local-paths` | `git grep` for local user or machine paths, such as `/Users/<name>/`, except the placeholder `you` | none |
| `run` | `go run` with the version stamped, passing `ARGS` to the binary | none |
| `mutation` | Gremlins (pinned to v0.5.1) on `internal/core` and then `internal/store`. Slow, and not run in CI | none |

Notes on the targets:

- **`build` and `build-linux`** make the `bin/` folder first. `bin/` is git-ignored.
- **`push`** needs `SERVER`. Without it the target stops with `Set SERVER, as in: make push SERVER=user@host`. The SSH port defaults to 22; set `SSH_PORT` to use another. Full steps, and where the file lands on the server, are in [Install-Server](Install-Server), Part 1.
- **`run`** is a convenience, for example `make run ARGS=version`.
- **`fmt` writes files.** Run `fmt-check` when you only want to see what is unformatted.
- **`cross` and `mutation`** are not run by the binary you use. `cross` only proves the code compiles for those systems, and `mutation` is for test quality.

The CI workflow runs `build`, `vet`, `fmt-check`, `test`, `cross`, `scan-invisible` and `scan-local-paths` on every pull request. [Checks](Checks) lists the workflows.

## Version strings

The version is set at build time from `git describe --tags --always --dirty`, with `dev` as the fallback:

| Build | Version string | Example |
|---|---|---|
| `make build` or `make build-linux` in a git checkout | the nearest tag, else the short commit hash | `cairn 8b651da` |
| The same, with uncommitted changes in the checkout | the same, plus `-dirty` | `cairn 8b651da-dirty` |
| A plain `go build ./cmd/cairn`, with no `-ldflags` | the default in the code | `cairn dev` |
| `make build` outside a git checkout | the fallback | `cairn dev` |

The repository has **no tags yet**, so today every `make build` prints a short commit hash. Once a tag exists, `git describe` reports the tag, the number of commits since it, and the hash, in the form `v1.2.0-3-g8b651da`. That form does not occur until a tag is made.

`cairn version`, `cairn -v` and `cairn --version` print the string. `cairn --help` does not.

## Systems

| Role | Systems | Notes |
|---|---|---|
| Build machine | Anything Go 1.26 runs on | `go.mod` declares `go 1.26.0`. CI reads the version from `go.mod` |
| Test machine | Linux, in CI (`ubuntu-latest`) | macOS is used by the developers, and CI does not run it |
| Server | Linux, `amd64` or `arm64` | `make build-linux`, with `LINUX_ARCH=arm64` for `aarch64` |
| Client | The client's own operating system and CPU | A Linux binary does not run on macOS, and the other way round |
| Cross-compile check | Windows, macOS, FreeBSD and OpenBSD | `make cross` only compiles and vets. Windows is not a supported target |

The SQLite driver is pure Go, so no C compiler is needed on any of them. The server's check that a database file is in use is built for `darwin`, `linux`, `freebsd` and `openbsd`. On other Unix systems the check reports that it cannot be done, and `restore` is weaker there. [DEPLOY.md](https://github.com/djm56/cairn-memory/blob/main/docs/DEPLOY.md) covers the server's requirements.

To check the server's CPU before you build for it, run `uname -m` on the server. `x86_64` means the default `amd64` build. `aarch64` means `LINUX_ARCH=arm64`.

## Where the binary goes

The binary is one file. Put it where your shell and your service manager look for it.

| Machine | Suggested path | Why |
|---|---|---|
| A developer machine | `~/.local/bin/cairn` | No root needed. The directory must be on your PATH |
| A developer machine, system-wide | `/usr/local/bin/cairn` | Needs root. On the PATH by default on most systems |
| A server | `/usr/local/bin/cairn` | The systemd unit runs this path |

The Makefile has no install target. Copy the file with `install`:

```bash
install -m 0755 bin/cairn ~/.local/bin/cairn
```

To check that the directory is on your PATH:

```bash
command -v cairn
```

**You should see** the full path of the file. If it prints nothing, add the directory to your PATH in your shell profile, and open a new terminal.

`cairn enable` runs `cairn` by name in the hooks it writes, so the directory must be on the PATH that Claude Code uses. `enable` warns when `cairn` is not on the PATH of the shell it runs in.

## Build a binary without the Makefile

If you need the same binary without `make`, the Makefile's `build` line is:

```bash
mkdir -p bin
go build -ldflags "-X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/cairn ./cmd/cairn
```

and `build-linux` adds `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` (or `arm64`) and writes `bin/cairn-linux`. The `make` form is the one CI uses, so prefer it.
