# The gate of mutate-go. CI runs the same targets, and `make check` runs
# them all. `make help` lists every target with its `## ` annotation.

.PHONY: help fmt build lint lint-go lint-md test test-min race cover check spec-sync spec-check

GO ?= go
GOLANGCI_LINT ?= golangci-lint
MARKDOWNLINT ?= markdownlint-cli2

# The oldest toolchain that the module's go line admits.
MIN_GO := go1.21.0

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "; printf "Targets:\n"} \
		/^[a-zA-Z][a-zA-Z_-]*:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST)

fmt: ## Format the Go sources
	$(GOLANGCI_LINT) fmt ./...

build: ## Compile every package
	$(GO) build ./...

lint: lint-go lint-md ## Run every linter

lint-go: ## Run golangci-lint, which runs vet
	$(GOLANGCI_LINT) run ./...

lint-md: ## Run markdownlint on every Markdown file
	$(MARKDOWNLINT) "**/*.md"

test: ## Run every test
	$(GO) test -count=1 ./...

test-min: ## Run `make cover` with the oldest toolchain that the module admits
	GOTOOLCHAIN=$(MIN_GO) $(MAKE) cover

race: ## Run every test under the race detector, one package at a time
	$(GO) test -race -count=1 -p 1 ./...

cover: ## Run every test, and fail unless the tests cover every statement
	$(GO) test -count=1 -coverprofile=cover.out ./...
	@awk 'NR > 1 && $$NF == 0 { print "not covered: " $$1; missed = 1 } END { exit missed }' cover.out

check: lint cover test-min race spec-check ## Run the gate that CI runs

spec-sync: ## Refresh the vendored definition from mutate-spec, and the files that the engine embeds
	./tools/spec-sync.sh conformance/spec go
	cp conformance/spec/VERSION conformance/spec/catalogue.json conformance/spec/protocol.json \
		conformance/spec/overlay.json internal/spec/

spec-check: ## Check that the vendored definition is intact, and report whether it is behind
	./tools/spec-check.sh conformance/spec

.DEFAULT_GOAL := help
