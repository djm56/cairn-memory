APP=cairn

# The version stamped into the binary: the nearest tag, else the commit, with
# -dirty for uncommitted changes. "dev" outside a git checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build build-linux push test vet fmt fmt-check cross scan-invisible scan-local-paths run mutation

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/cairn

# A Linux binary for a server, built on any machine. The SQLite driver is pure
# Go, so no C toolchain is needed. LINUX_ARCH is amd64 when the server's
# uname -m prints x86_64, and arm64 when it prints aarch64.
LINUX_ARCH ?= amd64

build-linux:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_ARCH) go build -ldflags "$(LDFLAGS)" -o bin/$(APP)-linux ./cmd/cairn

# Builds bin/cairn-linux, copies it to the home directory on SERVER, and runs
# its version there. It installs nothing: DEPLOY.md, Upgrade, does that.
# make push SERVER=user@host [SSH_PORT=4422]
SSH_PORT ?= 22

push:
	@test -n "$(SERVER)" || { echo "Set SERVER, as in: make push SERVER=user@host"; exit 1; }
	$(MAKE) build-linux
	scp -P $(SSH_PORT) bin/$(APP)-linux $(SERVER):$(APP)-linux
	ssh -p $(SSH_PORT) $(SERVER) ./$(APP)-linux version

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Fails and lists the files when anything is not gofmt-formatted; CI runs it.
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
	  echo "Not gofmt-formatted (run 'make fmt'):"; echo "$$unformatted"; exit 1; \
	fi

# Cross-compiles and vets the module for each other system the code is built for. go vet
# compiles the _test.go files too, which go build skips, so a break in a test file is caught.
# This is a compile check only: nothing here runs the binaries. Linux is the test target, and
# the other systems are compiled, not run. CI runs it on every pull request.
cross:
	GOOS=windows go build ./... && GOOS=windows go vet ./... && GOOS=darwin go build ./... && GOOS=darwin go vet ./... && GOOS=freebsd go build ./... && GOOS=freebsd go vet ./... && GOOS=openbsd go build ./... && GOOS=openbsd go vet ./...

# Fails and lists the files when a tracked file holds a bidirectional override, isolate or mark
# (U+202A to U+202E, U+2066 to U+2069, U+061C), a zero-width or invisible character (U+200B to
# U+200F, U+2060 to U+2065, U+180E, U+00AD), a tag character (U+E0000 to U+E007F), or a
# byte-order mark (U+FEFF). A byte-order mark is refused anywhere, even at the start of a file.
# Such characters can make source read differently from the bytes that run. git grep exits 0 on a
# match and 1 on none, so the result is inverted here. The locale must be UTF-8, or \x{...} is read
# as a byte and is too large; C.UTF-8 exists on the CI runner. CI runs this on every pull request.
scan-invisible:
	@hits="$$(LC_ALL=C.UTF-8 git grep -I -l -P '[\x{202A}-\x{202E}\x{2060}-\x{2069}\x{200B}-\x{200F}\x{FEFF}\x{061C}\x{180E}\x{00AD}\x{E0000}-\x{E007F}]')"; \
	status=$$?; \
	if [ $$status -eq 0 ]; then \
	  echo "Bidirectional or invisible characters found in these tracked files:"; echo "$$hits"; exit 1; \
	elif [ $$status -ne 1 ]; then \
	  echo "scan-invisible: git grep failed (exit $$status)"; exit $$status; \
	fi

# Fails and lists the lines when a tracked file holds a local path that names a user or a machine
# layout: a macOS, Linux or root home directory, a mounted volume, or a Windows user directory. The
# placeholder user "you" is allowed, so docs can show where a file goes (/Users/you/ and /home/you/).
# A path is skipped only when a letter, digit, "_", ".", "-", "~" or "$" comes right before it.
# After anything else, such as a backtick, a bracket, a pipe, a comma, a colon or a space, the path
# is caught. So internal/root/x and ../home/x are skipped, and a path in backticks or in a file URL
# is caught. The Windows form is a drive letter, a colon, slashes or backslashes, Users in any case,
# then more slashes or backslashes, and it follows the same rule. Not seen: a WSL path such as
# /mnt/c/Users/..., which has a letter before its slash, and a path after "..." or a period, which
# is skipped. The whole tree is scanned; no file is excluded. git grep exits 0 on a match and 1 on
# none, so the result is inverted here. CI runs this on every pull request.
# The ++ after users is possessive: a doubled separator before you is not split or flagged.
scan-local-paths:
	@hits="$$(git grep -I -n -P '(?<![A-Za-z0-9_.~$$-])(/(Users|home|root|Volumes)/(?!you/)|(?i:[a-z]:[\\/]+users[\\/]++)(?!you[\\/]))')"; \
	status=$$?; \
	if [ $$status -eq 0 ]; then \
	  echo "Local user or machine paths found in these tracked files (file:line:text):"; printf '%s\n' "$$hits"; exit 1; \
	elif [ $$status -ne 1 ]; then \
	  echo "scan-local-paths: git grep failed (exit $$status)"; exit $$status; \
	fi

# ARGS is passed to the binary: make run ARGS=version
run:
	go run -ldflags "$(LDFLAGS)" ./cmd/cairn $(ARGS)

# Mutation testing on core and store (issue #12), with Gremlins. It is pinned
# here, not in go.mod, so its dependencies stay out of the module. Slow, so it
# is not in CI. Gremlins takes one package path per run, hence two runs. Each
# prints its own score. See CONTRIBUTING.md, Mutation testing.
GREMLINS ?= github.com/go-gremlins/gremlins/cmd/gremlins@v0.5.1

# The timeout coefficient is raised. At the default, every mutant timed out
# against the short baseline run (first run: 355 of 359 core mutants).
mutation:
	go run $(GREMLINS) unleash --timeout-coefficient 20 ./internal/core
	go run $(GREMLINS) unleash --timeout-coefficient 20 ./internal/store
