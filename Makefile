.DEFAULT_GOAL := help

GO ?= go
BIN_DIR := bin
.PHONY: help
.PHONY: check build build-runiap build-runoauth build-runoauthprod generate
.PHONY: ko ko-runiap ko-runoauth ko-runoauthprod
.PHONY: test lint fmt vet
.PHONY: tidy clean run-runiap run-runoauth run-runoauthprod release-snapshot

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: generate fmt vet lint test ## Run local quality checks

generate: ## Generate templ Go code from .templ files
	$(GO) tool templ generate

build: generate build-runiap build-runoauth build-runoauthprod ## Build all binaries

build-runiap: ## Build runiap binary
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/runiap ./cmd/runiap

build-runoauth: ## Build runoauth binary
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/runoauth ./cmd/runoauth

build-runoauthprod: ## Build runoauthprod binary
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/runoauthprod ./cmd/runoauthprod

run-runiap: build-runiap ## Run runiap locally
	./$(BIN_DIR)/runiap

run-runoauth: build-runoauth ## Run runoauth locally
	./$(BIN_DIR)/runoauth

run-runoauthprod: build-runoauthprod ## Run runoauthprod locally
	./$(BIN_DIR)/runoauthprod

test: ## Run tests with race detector
	$(GO) test -race ./...

lint: ## Run golangci-lint
	@which golangci-lint >/dev/null || (echo "golangci-lint not installed. Install with: curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $$($(GO) env GOPATH)/bin" && exit 1)
	golangci-lint run

fmt: ## Format Go and templ code
	$(GO) fmt ./...
	$(GO) tool templ fmt .

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy go modules
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

ko: ko-runiap ko-runoauth ko-runoauthprod ## Build all images with ko

ko-runiap: generate ## Build runiap image with ko
	ko build ./cmd/runiap --bare --platform=linux/amd64

ko-runoauth: generate ## Build runoauth image with ko
	ko build ./cmd/runoauth --bare --platform=linux/amd64

ko-runoauthprod: generate ## Build runoauthprod image with ko
	ko build ./cmd/runoauthprod --bare --platform=linux/amd64

release-snapshot: ## Test goreleaser locally (no publish)
	goreleaser release --snapshot --clean

update: update-dep update-ga ## Update everything

update-dep: ## Update dependencies and vendor
	$(GO) get -u go@latest
	$(GO) get -u ./...

update-ga: ## Update pinned GitHub Actions workflow versions (ratchet)
	ratchet upgrade .github/workflows/release.yml
