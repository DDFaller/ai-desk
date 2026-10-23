BIN_DIR := bin
GO ?= $(shell which go 2>/dev/null || ( [ -f /tmp/go/bin/go ] && echo /tmp/go/bin/go ) || echo go)

.PHONY: all build build-submodules check-deps install-rtk test test-failover sync-upstream install clean

all: check-deps build build-submodules

check-deps:
	@which rtk >/dev/null 2>&1 || (echo "--> rtk not found. Installing RTK (Rust Token Killer)..." && $(MAKE) install-rtk)

install-rtk:
	@echo "--> Fetching and installing latest RTK (Rust Token Killer)..."
	@curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/master/install.sh | bash
	@echo "✓ RTK installed to ~/.local/bin/rtk"

build: check-deps
	@mkdir -p $(BIN_DIR)
	@echo "--> Building ai-desk..."
	@$(GO) build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/ai-desk ./cmd/ai-desk
	@echo "--> Building desk-shim..."
	@$(GO) build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/desk-shim ./cmd/desk-shim
	@echo "✓ Native AI Desk binaries compiled into $(BIN_DIR)/"

build-submodules:
	@mkdir -p $(BIN_DIR)
	@echo "--> Compiling pristine submodules/agent-deck..."
	@cd submodules/agent-deck && $(GO) build -trimpath -ldflags "-s -w" -o ../../$(BIN_DIR)/agent-deck ./cmd/agent-deck
	@echo "--> Compiling pristine submodules/agy-swap..."
	@cd submodules/agy-swap && $(GO) build -trimpath -ldflags "-s -w" -o ../../$(BIN_DIR)/agy-swap ./cmd/agy-swap
	@echo "✓ Pristine upstream binaries compiled into $(BIN_DIR)/"

test:
	@echo "--> Running unit test suite..."
	@$(GO) test -v ./pkg/...

test-failover: build
	@./scripts/test-failover.sh

sync-upstream:
	@./scripts/sync-upstream.sh

install: build
	@mkdir -p $(HOME)/.local/bin
	@ln -sf $(CURDIR)/$(BIN_DIR)/ai-desk $(HOME)/.local/bin/ai-desk
	@ln -sf $(CURDIR)/$(BIN_DIR)/desk-shim $(HOME)/.local/bin/desk-shim
	@echo "✓ Installed ai-desk and desk-shim to ~/.local/bin/"

clean:
	@rm -rf $(BIN_DIR)
