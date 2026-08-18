BINARY_NAME := trenchcoat
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# The linter version CI runs. Read from the workflow rather than copied, so
# Renovate's bump there is the only place it has to happen. `golangci-lint
# version --short` prints it without the leading v.
CI_GOLANGCI_LINT_VERSION := $(shell sed -n 's/^  GOLANGCI_LINT_VERSION: "v\{0,1\}\(.*\)"/\1/p' .github/workflows/ci.yaml)

.PHONY: build test coverage lint clean

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/trenchcoat/

test:
	go test -v -count=1 -race ./...

coverage:
	./scripts/coverage-report.sh --html

# .golangci.yml pins the linter set, not the linter. A different golangci-lint
# carries a different staticcheck vintage, which is the skew that shows up as a
# failure CI cannot reproduce, so say so rather than let it surprise someone.
lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	@local=$$(golangci-lint version --short 2>/dev/null); \
	if [ -n "$(CI_GOLANGCI_LINT_VERSION)" ] && [ "$$local" != "$(CI_GOLANGCI_LINT_VERSION)" ]; then \
		echo "warning: golangci-lint $$local locally, $(CI_GOLANGCI_LINT_VERSION) in CI — results may differ"; \
	fi
	golangci-lint run ./...

clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html coverage-test.log
	go clean -testcache
