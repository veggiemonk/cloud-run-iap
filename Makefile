.DEFAULT_GOAL := help

GO ?= go
BIN_DIR := bin
GO_VERSION := $(shell $(GO) env GOVERSION | sed 's/go//' | cut -d. -f1,2)

.PHONY: help
.PHONY: check build docker generate
.PHONY: test lint fmt vet
.PHONY: tidy clean run

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: generate fmt vet lint test ## Run local quality checks

generate: ## Generate templ Go code from .templ files
	$(GO) tool templ generate

build: generate ## Build server binary
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/runiap .

run: build ## Run server locally
	./$(BIN_DIR)/runiap

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

docker: ## Build container image
	docker build --build-arg GO_VERSION=$(GO_VERSION) -t runiap .
