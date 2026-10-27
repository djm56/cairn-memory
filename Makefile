APP=cairn

# The version stamped into the binary: the nearest tag, else the commit, with
# -dirty for uncommitted changes. "dev" outside a git checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet fmt fmt-check run

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
