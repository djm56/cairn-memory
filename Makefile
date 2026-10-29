APP=cairn

# The version stamped into the binary: the nearest tag, else the commit, with
# -dirty for uncommitted changes. "dev" outside a git checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet fmt fmt-check run mutation

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
