APP=cairn

# The version stamped into the binary: the nearest tag, else the commit, with
# -dirty for uncommitted changes. "dev" outside a git checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet fmt fmt-check scan-invisible run mutation

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/cairn

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

# Fails and lists the files when a tracked file holds a bidirectional override or isolate
# (U+202A to U+202E, U+2066 to U+2069), a zero-width or invisible character (U+200B to U+200F,
# U+2060 to U+2064), or a byte-order mark (U+FEFF). A byte-order mark is refused anywhere, even at
# the start of a file. Such characters can make source read differently from the bytes that run.
# git grep exits 0 on a match and 1 on none, so the result is inverted here. The locale must be
# UTF-8, or \x{...} is read as a byte and is too large; C.UTF-8 exists on the CI runner. CI runs
# this on every pull request.
scan-invisible:
	@hits="$$(LC_ALL=C.UTF-8 git grep -I -l -P '[\x{202A}-\x{202E}\x{2060}-\x{2069}\x{200B}-\x{200F}\x{FEFF}]')"; \
	status=$$?; \
	if [ $$status -eq 0 ]; then \
	  echo "Bidirectional or invisible characters found in these tracked files:"; echo "$$hits"; exit 1; \
	elif [ $$status -ne 1 ]; then \
	  echo "scan-invisible: git grep failed (exit $$status)"; exit $$status; \
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
