BINARY_NAME := trenchcoat
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build test coverage lint clean

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/trenchcoat/

test:
	go test -v -count=1 -race ./...

coverage:
	go test -v -count=1 -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"

lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html
	go clean -testcache
