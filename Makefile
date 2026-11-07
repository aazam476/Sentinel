# SPDX-License-Identifier: Apache-2.0
#
# Every command a contributor or CI needs. Run `make help` for the list.
# Recipes use bash so they behave the same on Linux, macOS and Windows (Git Bash).

SHELL := bash
.SHELLFLAGS := -euo pipefail -c

GO ?= go
NPM ?= npm
FUZZTIME ?= 30s
WEB_DIST := internal/server/web/dist
EXE := $(if $(filter Windows_NT,$(OS)),.exe,)

# Release targets for `make build-all`.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this list of commands
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-20s %s\n", $$1, $$2}'

# ---- Building ----------------------------------------------------------------

web/node_modules: web/package.json web/package-lock.json
	cd web && $(NPM) ci
	touch web/node_modules

.PHONY: web-deps
web-deps: web/node_modules ## Install the web UI's npm packages

.PHONY: web
web: web/node_modules ## Build the web UI and copy it to where the server embeds it
	cd web && $(NPM) run build
	scripts/copy-web-build.sh web/dist $(WEB_DIST)

.PHONY: build
build: web ## Build both binaries for this machine into bin/
	CGO_ENABLED=0 $(GO) build -o bin/sentinel-server$(EXE) ./cmd/sentinel-server
	CGO_ENABLED=0 $(GO) build -o bin/sentinel-agent$(EXE) ./cmd/sentinel-agent

.PHONY: build-all
build-all: web ## Cross-compile both binaries for every release platform into bin/<os>-<arch>/
	scripts/build-all.sh $(PLATFORMS)

.PHONY: dev
dev: ## Start the server and the web UI dev server with hot reload
	scripts/run-dev.sh

# ---- Linting -----------------------------------------------------------------

.PHONY: lint
lint: lint-go lint-web check-headers check-generated-types ## Run every linter and formatting check

.PHONY: lint-go
lint-go: ## gofmt, go vet and golangci-lint
	scripts/check-gofmt.sh
	$(GO) vet ./...
	$(GO) vet -tags integration ./...
	golangci-lint run --build-tags integration ./...

.PHONY: lint-web
lint-web: web/node_modules ## eslint, tsc and prettier
	cd web && $(NPM) run lint
	cd web && $(NPM) run typecheck
	cd web && $(NPM) run format:check

.PHONY: gen-types
gen-types: ## Regenerate web/src/api/types.gen.ts from the Go API types
	scripts/gen-types.sh

.PHONY: check-generated-types
check-generated-types: ## Check web/src/api/types.gen.ts matches the Go API types
	scripts/check-generated-types.sh

.PHONY: check-headers
check-headers: ## Check every source file starts with the SPDX license header
	scripts/check-license-headers.sh

.PHONY: check-licenses
check-licenses: web/node_modules ## Check no dependency is GPL, AGPL or LGPL licensed
	scripts/check-licenses.sh

.PHONY: check-sec-coverage
check-sec-coverage: ## Check every SEC-xx item in scope has a test named after it
	scripts/check-sec-coverage.sh

.PHONY: check-no-prototype
check-no-prototype: web ## Check the production web build contains no design variant picker
	scripts/check-no-prototype.sh $(WEB_DIST)

# ---- Testing -----------------------------------------------------------------

.PHONY: test
test: test-go test-web ## Unit tests (white- and black-box) for Go and the web UI

.PHONY: test-go
test-go: ## Go unit tests with the race detector
	$(GO) test -race ./...

.PHONY: test-web
test-web: web/node_modules ## Web UI unit and component tests
	cd web && $(NPM) test

.PHONY: test-integration
test-integration: web ## HTTP, protocol and command-line contract tests
	$(GO) test -race -tags integration ./...

.PHONY: test-e2e
test-e2e: build ## Playwright end-to-end tests against the real server
	cd web && npx playwright test

.PHONY: test-all
test-all: lint test test-integration test-e2e check-sec-coverage check-licenses check-no-prototype ## All tests, linters and checks
	@echo "test-all: everything passed"

.PHONY: fuzz
fuzz: ## Run every fuzz target for FUZZTIME (default 30s)
	scripts/fuzz.sh $(FUZZTIME)

.PHONY: cover
cover: web/node_modules ## Coverage reports, checked against the minimum thresholds
	scripts/check-coverage.sh
	cd web && $(NPM) run test:coverage

# ---- Cleaning ----------------------------------------------------------------

.PHONY: clean
clean: ## Remove build output
	scripts/clean.sh
