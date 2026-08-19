BINARY      := myrepl
BIN_DIR     := bin
BIN         := $(BIN_DIR)/$(BINARY)
PKG         := ./...
MAIN        := .

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.DEFAULT_GOAL := build

## help: list available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F': ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

## build: compile the binary into ./bin
.PHONY: build
build: $(BIN)

$(BIN): $(shell find . -name '*.go' -not -name '*_test.go') go.mod go.sum
	@mkdir -p $(BIN_DIR)
	go build -ldflags '$(LDFLAGS)' -o $(BIN) $(MAIN)

## run: build and run (pass args via ARGS="...")
.PHONY: run
run: build
	./$(BIN) $(ARGS)

## test: run all tests
.PHONY: test
test:
	go test $(PKG)

## test-race: run tests with the race detector and coverage
.PHONY: test-race
test-race:
	go test -race -cover $(PKG)

## vet: run go vet
.PHONY: vet
vet:
	go vet $(PKG)

## fmt: format all Go source
.PHONY: fmt
fmt:
	gofmt -s -w .

## fmt-check: fail if any file is not gofmt-clean
.PHONY: fmt-check
fmt-check:
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi

## tidy: tidy and verify go.mod/go.sum
.PHONY: tidy
tidy:
	go mod tidy
	go mod verify

## check: fmt-check, vet, and test (use in CI / pre-commit)
.PHONY: check
check: fmt-check vet test

## install: install the binary into GOBIN/GOPATH bin
.PHONY: install
install:
	go install -ldflags '$(LDFLAGS)' $(MAIN)

## clean: remove build artifacts
.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
	go clean
