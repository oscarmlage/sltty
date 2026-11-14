
BINARY   := sltty
PKG      := ./cmd/sltty
BIN_DIR  := bin
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
PREFIX   ?= $(HOME)/.local


.DEFAULT_GOAL := help
.PHONY: help build run demo test cover cover-html fmt vet check install clean

help: ## list targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## build bin/sltty with the local Go
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

run: build ## build and open the TUI
	./$(BIN_DIR)/$(BINARY) $(ARGS)

demo: build ## re-record docs/assets/demo/*.gif in kitty (macOS; needs kitty, ffmpeg)
	python3 scripts/record-demo.py docs/assets/demo/*.tape

test: ## run the tests
	go test ./...

cover: ## tests with the total coverage (all packages count, whoever tests them)
	go test -coverpkg=./... -coverprofile=coverage.out ./... && go tool cover -func coverage.out | tail -1

cover-html: cover ## open the coverage report in a browser
	go tool cover -html coverage.out

fmt: ## gofmt the tree
	gofmt -w cmd internal

vet: ## go vet
	go vet ./...

check: ## gofmt check, vet and tests (what CI runs)
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "run: make fmt"; exit 1; }
	go vet ./...
	go test ./...

install: ## install bin/sltty into $(PREFIX)/bin (default ~/.local/bin)
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN_DIR)/$(BINARY) $(PREFIX)/bin/$(BINARY)

clean: ## remove build output
	rm -rf $(BIN_DIR) coverage.out

