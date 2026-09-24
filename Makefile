BINARY := synchro
CMD := ./cmd/synchro
GO_FILES := $(shell find . -name '*.go' -not -path './vendor/*')
VERSION := $(shell grep -m1 -oE '\[[0-9]+\.[0-9]+\.[0-9]+\]' CHANGELOG.md | tr -d '[]')
LDFLAGS := -X github.com/szok/synchro/internal/version.Version=$(VERSION)

# VS Code target:GOOS/GOARCH pairs for per-platform extension packages.
VSCODE_TARGETS := darwin-arm64:darwin/arm64 darwin-x64:darwin/amd64 \
	linux-x64:linux/amd64 linux-arm64:linux/arm64 win32-x64:windows/amd64

.PHONY: help build build-windows test test-cover fmt fmt-check vet check clean run vscode-install-deps vscode-bin vscode-package

help: ## Show available commands.
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "%-20s %s\n", $$1, $$2}'

build: ## Build the Synchro binary.
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

build-windows: ## Cross-compile the Windows amd64 binary.
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY).exe $(CMD)

test: ## Run all tests.
	go test ./...

test-cover: ## Run tests and write coverage.out.
	go test -coverprofile=coverage.out ./...

fmt: ## Format Go source files.
	go fmt ./...

fmt-check: ## Report Go files that need formatting.
	@test -z "$(shell gofmt -l $(GO_FILES))" || \
		(printf 'The following files need gofmt:\n'; gofmt -l $(GO_FILES); exit 1)

vet: ## Run Go static analysis.
	go vet ./...

check: fmt-check vet test ## Run formatting, static analysis, and tests.

run: build ## Build and show CLI help.
	./$(BINARY) --help

vscode-install-deps: ## Install the VS Code extension's npm dependencies (npm ci).
	cd vscode && npm ci

vscode-bin: ## Build the binary for this machine into vscode/bin (for F5 debugging).
	mkdir -p vscode/bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o vscode/bin/$(BINARY)$(shell go env GOEXE) $(CMD)

vscode-package: ## Build one VS Code extension package (.vsix) per platform into vscode/dist (run vscode-install-deps first).
	mkdir -p vscode/dist
	@set -e; for pair in $(VSCODE_TARGETS); do \
		target=$${pair%%:*}; platform=$${pair#*:}; goos=$${platform%/*}; goarch=$${platform#*/}; \
		ext=; [ "$$goos" = windows ] && ext=.exe; \
		rm -rf vscode/bin && mkdir -p vscode/bin; \
		echo "==> $$target"; \
		GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o vscode/bin/$(BINARY)$$ext $(CMD); \
		(cd vscode && npx vsce package --target $$target --out dist/); \
	done
	rm -rf vscode/bin

clean: ## Remove build and coverage artifacts.
	rm -f $(BINARY) $(BINARY).exe coverage.out
	rm -rf vscode/bin vscode/out vscode/dist
