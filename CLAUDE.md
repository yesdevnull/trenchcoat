# Trenchcoat

Extensible mock, and proxy-to-mock, HTTP server written in Go.

## Project Overview

Trenchcoat is a CLI tool with two modes:

1. **Serve mode** — mock HTTP server matching requests against "coat" definitions
2. **Proxy mode** — HTTP proxy capturing request/response pairs as coat files

Module path: `github.com/yesdevnull/trenchcoat`

## Repository Structure

```
cmd/trenchcoat/     CLI entrypoint (cobra commands: serve, proxy, validate)
internal/
  coat/             Types, parsing, validation for coat files
  config/           Viper-based config file loading
  matcher/          Request matching engine (exact, glob, regex URI)
  proxy/            Proxy capture server
  server/           Mock HTTP server
examples/
  go-tests/         Example test suite using the programmatic API
docs/
  demo.md           CLI demo walkthrough
  ROADMAP.md        Future feature plans
trenchcoat.go       Public API package for Go test integration
```

## Development

### Requirements

- Go 1.25.x (pinned in CI: 1.25.7)
- golangci-lint v2.10.1+

### Installing Go

If Go 1.25+ is not installed or the auto-download via `GOTOOLCHAIN` fails (e.g.
due to DNS/network restrictions), install manually:

```bash
# Download (linux/amd64 — adjust for your platform)
curl -fSL -o /tmp/go1.25.7.linux-amd64.tar.gz "https://go.dev/dl/go1.25.7.linux-amd64.tar.gz"

# Install (removes any previous Go installation in /usr/local/go)
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go1.25.7.linux-amd64.tar.gz

# Verify
go version   # should print "go version go1.25.7 linux/amd64"
```

Ensure `/usr/local/go/bin` is in your `PATH`.

### Commands

```bash
make build          # Build binary
make test           # Run tests
make lint           # Run linter
make clean          # Clean build artifacts

go test ./...                           # Run all tests
go test -v -race -count=1 ./...        # Verbose with race detector
go vet ./...                            # Static analysis
gofmt -w .                              # Format code
goimports -w .                          # Fix imports
golangci-lint run ./...                 # Lint
govulncheck ./...                       # Vulnerability check
```

### Build with Version Info

```bash
go build -ldflags "-s -w \
  -X main.version=$(git describe --tags --always --dirty) \
  -X main.commit=$(git rev-parse --short HEAD) \
  -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  ./cmd/trenchcoat/
```

## TDD Methodology

Use Red/Green/Refactor throughout:

1. **Red** — Write a failing test that defines expected behaviour
2. **Green** — Write the minimum code to pass
3. **Refactor** — Clean up while keeping tests green

Every feature begins with a test. Do not write implementation code without a corresponding failing test first.

## Architecture Notes

### Coat Specification

Coats are YAML or JSON files defining request/response mocks. Schema:

```yaml
coats:
  - name: "descriptive-name"
    request:
      method: GET                    # optional, default GET. Supports ANY.
      uri: "/api/v1/users"          # mandatory. Exact, glob (*/?), or regex (~/).
      headers:                       # optional, subset match with glob values
        Authorization: "Bearer *"
      query:                         # optional, string or map with glob values
        page: "1"
    response:
      code: 200
      headers:
        Content-Type: "application/json"
      body: '{"users": []}'         # or body_file: "./fixtures/data.json"
      delay_ms: 0
    # OR for sequences (mutually exclusive with response):
    responses:
      - code: 503
        body: "unavailable"
      - code: 200
        body: "ok"
    sequence: cycle                  # cycle (default) or once
```

### URI Matching Modes

| Mode  | Syntax          | Example                    |
|-------|-----------------|----------------------------|
| Exact | Plain string    | `/api/v1/users`            |
| Glob  | Contains `*`/?  | `/api/v1/users/*`          |
| Regex | `~/` prefix     | `~/api/v1/users/\d+`       |

### Match Precedence (highest to lowest)

1. Exact URI + method + headers + query
2. Exact URI + method + fewer qualifiers
3. Glob URI (longer literal prefix wins)
4. Regex URI (file-definition order)
5. `method: ANY` ranks below method-specific at same specificity

### Key Dependencies

| Package         | Purpose                        |
|-----------------|--------------------------------|
| cobra           | CLI framework                  |
| viper           | Config file and flag binding   |
| fsnotify        | Hot-reload file watching       |
| slog (stdlib)   | Structured logging             |
| gopkg.in/yaml.v3 | YAML parsing                 |

### Proxy Capture

- Respects `http_proxy`/`https_proxy`/`no_proxy` env vars
- File naming: `{METHOD}_{sanitised_path}_{status}.yaml`
- Dedupe strategies: `overwrite` (stable filename), `skip`, `append`
- Headers in `--strip-headers` are redacted from captures

### Programmatic API (for Go tests)

```go
srv := trenchcoat.NewServer(
    trenchcoat.WithCoat(trenchcoat.Coat{
        Name:    "get-users",
        Request: trenchcoat.Request{Method: "GET", URI: "/api/v1/users"},
        Response: &trenchcoat.Response{
            Code: 200,
            Body: `{"users": []}`,
        },
    }),
)
srv.Start(t) // registers t.Cleanup to stop the server
// srv.URL contains "http://127.0.0.1:<port>"
```

## Testing Expectations

- Unit tests for matcher: exact, glob, regex URI; method+ANY; header globs; query matching; precedence
- Unit tests for coat parsing/validation (YAML, JSON, mutual exclusivity rules)
- Integration tests for serve mode (start server, send requests, assert responses)
- Integration tests for proxy mode (proxy through, assert captured coat files)
- Tests for response sequences (cycle and once modes)
- Tests for hot-reload (modify coat file on disk, verify server picks up changes)
- Tests for TLS (self-signed cert)

## CI

GitHub Actions workflow at `.github/workflows/trenchcoat-ci.yaml` runs:
- **Test**: `go test -race -coverprofile`
- **Lint**: golangci-lint v2.10.1
- **Vet**: `go vet`, `go mod tidy` check, `govulncheck`
- **Format**: `gofmt`, `goimports`
- **Build**: Cross-compile linux/darwin x amd64/arm64 with ldflags

## Pre-commit Requirements

Before every commit, run the following and fix any issues:

```bash
gofmt -w .                  # Format all Go code
goimports -w .              # Fix imports
golangci-lint run ./...     # Lint
go vet ./...                # Static analysis
go test -race ./...         # Run tests with race detector
```

All Go source files **must** be formatted with `gofmt` and `goimports` before
committing. Unformatted code must not be committed.

## Conventions

- Use `net/http` directly — no web frameworks
- Use `slog` for logging (text and JSON formats)
- Distribute as a single static binary (CGO_ENABLED=0)
- Support Linux and macOS
- Coat files must be human-readable and hand-editable
- `body_file` paths resolve relative to the coat file's location
- Graceful shutdown on SIGINT/SIGTERM with 10s drain timeout
- Use `net.Listen("tcp4", addr)` for IPv4-only binding
- `sync.WaitGroup.Go()` (Go 1.25) for fire-and-forget goroutines
