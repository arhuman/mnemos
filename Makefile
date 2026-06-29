# mnemos — local-first memory for AI agents (single cgo-free Go binary)

BINARY     := mnemos
CMD        := ./cmd/mnemos
BIN_DIR    := bin
SRC_DIRS   := internal cmd
COVER_MIN  := 80
COVER_OUT  := coverage.out
COVER_HTML := coverage.html
SKILL_SRC  := skills/mnemos-okf
SKILL_DEST := $(HOME)/.claude/skills/mnemos-okf
TAPES      := docs/demo.tape docs/demo-mcp.tape

# Pinned dev-tool versions installed by `make tools` (reproducible audits).
GOLANGCI_VERSION    := v2.12.2
GOVULNCHECK_VERSION := v1.1.4

# Version metadata injected into internal/version via -ldflags -X. VERSION is a
# git describe (tag-or-commit), falling back to "dev" outside a git checkout.
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VPKG       := github.com/arhuman/mnemos/internal/version
LDFLAGS    := -ldflags "-X $(VPKG).Version=$(VERSION) -X $(VPKG).GitCommit=$(COMMIT) -X $(VPKG).BuildDate=$(BUILD_DATE)"

.PHONY: build build-embed test bench bench-smoke cover tidy audit tools install install-embed install-skill demo demo-check clean help

# First target is the default (`make` == `make build`).
## build: compile the single binary (cgo-free) into bin/
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD)

## build-embed: compile with semantic embedding support (cgo-free, -tags embed)
build-embed:
	CGO_ENABLED=0 go build -tags embed $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD)

## test: run all tests with the race detector
test:
	go test -race ./...

## bench: run all benchmarks with allocation stats (perf measurement, not gated)
bench:
	go test -run='^$$' -bench=. -benchmem ./...

## bench-smoke: run every benchmark once (fast) so they cannot rot; CI runs this
bench-smoke:
	go test -run='^$$' -bench=. -benchmem -benchtime=1x ./...

## cover: run tests with coverage, write reports, fail under COVER_MIN%
cover:
	go test -race -covermode=atomic -coverprofile=$(COVER_OUT) ./...
	go tool cover -func=$(COVER_OUT)
	go tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@total=$$(go tool cover -func=$(COVER_OUT) | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	echo "total coverage: $$total% (minimum $(COVER_MIN)%)"; \
	awk "BEGIN{ exit !($$total+0 >= $(COVER_MIN)) }" || \
	  { echo "FAIL: total coverage $$total% is below the $(COVER_MIN)% gate"; exit 1; }

## tidy: tidy go.mod and gofmt the source tree
tidy:
	go mod tidy
	gofmt -w $(SRC_DIRS)

## tools: install pinned Go dev tools into GOBIN
tools:
	@echo "Installing Go tools..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@echo "Tools installed in $(shell go env GOBIN || go env GOPATH)/bin"

## audit: full quality gate (golangci-lint incl. govet+staticcheck, mod verify, vuln scan, race+coverage gate)
audit:
	go mod verify
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	$(MAKE) cover

## install: install the binary into GOBIN (cgo-free)
install:
	CGO_ENABLED=0 go install $(LDFLAGS) $(CMD)

## install-embed: install with semantic embedding support (cgo-free, -tags embed)
install-embed:
	CGO_ENABLED=0 go install -tags embed $(LDFLAGS) $(CMD)

## install-skill: install the Claude Code mnemos-okf skill into ~/.claude/skills/
install-skill:
	rm -rf $(SKILL_DEST)
	mkdir -p $(SKILL_DEST)
	cp -R $(SKILL_SRC)/. $(SKILL_DEST)/
	@echo "installed skill -> $(SKILL_DEST)"

## demo: render the README demo GIFs from the VHS tapes (needs vhs + mnemos on PATH; demo-mcp also needs claude)
demo:
	@which vhs > /dev/null || { echo "vhs not found: install github.com/charmbracelet/vhs"; exit 1; }
	@for tape in $(TAPES); do echo "rendering $$tape"; vhs $$tape; done

## demo-check: validate the demo tapes parse (cheap CI guard; does NOT byte-compare rendered GIFs)
demo-check:
	@which vhs > /dev/null || { echo "vhs not found: install github.com/charmbracelet/vhs"; exit 1; }
	@for tape in $(TAPES); do echo "validating $$tape"; vhs validate $$tape; done

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR) $(COVER_OUT) $(COVER_HTML)

## help: list available targets
help:
	@grep -E '^## [a-z-]+:' $(MAKEFILE_LIST) | sed -E 's/^## //' | sort
