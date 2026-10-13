VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64

# from the latest tag (v1.2.3 -> 1.2.3); override with PKG_VERSION=x.y.z
PKG_VERSION ?= $(or $(patsubst v%,%,$(shell git describe --tags --abbrev=0 2>/dev/null)),0.1.0)
MAINTAINER ?= ducktainer <ducktainer@localhost>

.DEFAULT_GOAL := help

.PHONY: help all web build build-pam dist packages dev run serve ui test test-unit test-race test-web clean

help: ## Show this help message
	@echo "ducktainer — lightweight Docker management"
	@echo ""
	@echo "Usage: make [target]"
	@awk 'BEGIN {FS = ":.*?## "} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)} \
		/^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ Running the service
run: build ## Run ducktainer serve locally (builds first; args via ARGS="...")
	./bin/ducktainer serve $(ARGS)

serve: run ## Alias for 'run'

ui: build ## Run ducktainer ui and open browser (builds first)
	./bin/ducktainer ui $(ARGS)

##@ Build targets
all: build ## Build web assets and binary (default build)

web/node_modules: web/package.json
	cd web && (bun install || npm install)
	@touch $@

web: web/node_modules ## Build web frontend assets
	cd web && (bun run build || npm run build)

build: web ## Build static binary in bin/ducktainer (no PAM)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ducktainer .

# Local binary with PAM (needs gcc and pam-devel / libpam0g-dev)
build-pam: web ## Build binary with PAM support (needs gcc and pam headers)
	CGO_ENABLED=1 go build -trimpath -tags pam -ldflags "$(LDFLAGS)" -o bin/ducktainer .

# rpm + deb (amd64, arm64) with PAM, built in a container: needs only Docker
packages: web ## Build rpm and deb packages (amd64, arm64) using Docker
	docker build -t ducktainer-pkg-builder packaging
	docker run --rm -u $$(id -u):$$(id -g) -v $(CURDIR):/src -w /src \
		-e VERSION=$(PKG_VERSION) -e MAINTAINER="$(MAINTAINER)" ducktainer-pkg-builder packaging/build.sh

# Static binaries for every platform in dist/ (no PAM: token login only)
dist: web ## Build static binaries for all platforms in dist/
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "→ $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/ducktainer-$$os-$$arch$$ext . || exit 1; \
	done
	@cd dist && sha256sum ducktainer-* > SHA256SUMS

##@ Development
dev: ## Run frontend dev server with hot reload against ducktainer serve
	cd web && bun run dev

##@ Testing
test: test-unit test-web ## Run all tests (Go unit tests and frontend typecheck)

test-unit: ## Run Go unit tests
	go test -v ./...

test-race: ## Run Go unit tests with data race detector
	go test -race -v ./...

test-web: web/node_modules ## Run frontend typecheck
	cd web && (bun run typecheck || npm run typecheck)

##@ Cleanup
clean: ## Remove build artifacts
	rm -rf bin dist web/dist/*
	@mkdir -p web/dist && touch web/dist/.gitkeep
