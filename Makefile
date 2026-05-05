.PHONY: build test lint fmt clean e2e

# Version metadata injected at link time. Override any of these on the
# `make` command line (e.g., `make build VERSION=v0.7.0-rc1`) or let them
# default to git-derived values.
#
# Goreleaser uses its own equivalent ldflags from .goreleaser.yaml — this
# block is for local `make build`, which would otherwise produce a binary
# reporting "0.0.1-dev (commit unknown, built unknown)".
#
# Note: `runtime/debug.ReadBuildInfo()` in cmd/agent-gate/version.go also
# falls back to vcs metadata for plain `go build`, but Go 1.25 has a known
# worktree bug where the embedded vcs.revision is the parent repo's HEAD
# rather than the worktree's HEAD. Setting these via ldflags sidesteps it.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.1-dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell git log -1 --format=%cI HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o agent-gate ./cmd/agent-gate

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f agent-gate

e2e:
	go test -timeout 60s ./internal/e2e/...
