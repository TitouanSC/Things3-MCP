GOLANGCI_LINT_VERSION := v2.15.0
GOVULNCHECK_VERSION   := v1.9.0
VERSION               ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test cover lint vuln secrets check hooks

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/things-mcp ./cmd/things-mcp

test:
	go test -race ./...

cover:
	go test -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

secrets:
	gitleaks git --redact --no-banner .

# Everything CI runs.
check: lint test vuln secrets

hooks:
	git config core.hooksPath .githooks
