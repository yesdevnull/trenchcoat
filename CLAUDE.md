# Trenchcoat

Extensible mock, and proxy-to-mock, HTTP server written in Go.

## Project Overview

Trenchcoat is a CLI tool with two modes:

1. **Serve mode** — mock HTTP server matching requests against "coat" definitions
2. **Proxy mode** — HTTP proxy capturing request/response pairs as coat files

Module path: `github.com/yesdevnull/trenchcoat`

## Repository Structure

```
cmd/trenchcoat/           CLI entrypoint (cobra commands: serve, proxy, validate)
  main.go                 Root command, signal handling, version info
  serve.go                Serve subcommand with hot-reload file watching
  proxy.go                Proxy subcommand
  validate.go             Validate subcommand
  commands_test.go        CLI integration tests
internal/
  coat/                   Types, parsing, validation for coat files
    types.go              Core types: File, Coat, Request, Response, QueryField
    parse.go              YAML/JSON file parsing
    load.go               LoadPaths: loads coats from files and directories
    validate.go           Schema validation (mutual exclusivity, regex, etc.)
    query.go              QueryField YAML/JSON unmarshalling
  config/                 Viper-based config file loading
    config.go             Config discovery: --config > .trenchcoat.yaml > ~/.config/trenchcoat/config.yaml
  matcher/                Request matching engine (exact, glob, regex URI)
    matcher.go            Match logic, precedence scoring, sequence state
  proxy/                  Proxy capture server
    proxy.go              HTTP proxy, upstream forwarding, coat file capture
  server/                 Mock HTTP server
    server.go             HTTP server, request handling, body_file resolution
examples/
  go-tests/               Example test suite using the programmatic API
    example_test.go       Basic mock, multiple coats, headers, sequences, globs
docs/
  demo.md                 CLI demo walkthrough
  ROADMAP.md              Future feature plans
  test-coverage-analysis.md  Coverage report and test inventory
trenchcoat.go             Public API package for Go test integration
trenchcoat_test.go        Public API tests
.github/workflows/ci.yaml  CI pipeline (test, lint, vet, format, build)
.goreleaser.yaml          GoReleaser config for cross-platform releases
renovate.json             Renovate dependency auto-update config
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
curl -fSL -o /tmp/go1.25.8.linux-amd64.tar.gz "https://go.dev/dl/go1.25.8.linux-amd64.tar.gz"

# Install (removes any previous Go installation in /usr/local/go)
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go1.25.8.linux-amd64.tar.gz

# Verify
go version   # should print "go version go1.25.8 linux/amd64"
```

Ensure `/usr/local/go/bin` is in your `PATH`.

### Commands

```bash
make build          # Build binary
make test           # Run tests (verbose, race detector)
make coverage       # Run tests with coverage, generate HTML report
make lint           # Run golangci-lint
make clean          # Clean build artifacts and test cache

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

### CLI Commands

The CLI uses cobra with three subcommands:

**`trenchcoat serve`** — Start the mock HTTP server
- `--coats` — Paths to coat files or directories to load
- `--port` — Port to listen on (default: 8080)
- `--tls-cert` / `--tls-key` — TLS certificate and key (must be provided together)
- `--watch` — Watch coat files for changes and hot-reload
- `--verbose` — Log each incoming request and match result
- `--log-format` — Log format: `text` (default) or `json`

**`trenchcoat proxy <upstream-url>`** — Start in proxy capture mode
- `--port` — Port to listen on (default: 8080)
- `--write-dir` — Directory to write captured coat files (default: `.`)
- `--filter` — Only capture requests whose URI matches this glob pattern
- `--strip-headers` — Headers to redact (default: `Authorization`, `Cookie`, `Set-Cookie`)
- `--no-headers` — Omit all headers from captured coat files (mutually exclusive with `--strip-headers`)
- `--capture-body` — Capture request body in coat files (default: `true`)
- `--dedupe` — Deduplication strategy: `overwrite` (default), `skip`, or `append`
- `--verbose` — Log each proxied request and capture event
- `--log-format` — Log format: `text` (default) or `json`

**`trenchcoat validate <path>...`** — Validate coat files for schema correctness

All commands support `--config` (global flag) for explicit config file path.

### Configuration File Discovery

Config files are discovered in this order (first found wins):

1. `--config` flag (explicit path)
2. `.trenchcoat.yaml` / `.trenchcoat.yml` in current working directory
3. `~/.config/trenchcoat/config.yaml`

No config file is required — the tool works with CLI flags alone.

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
      body: '{"name": "alice"}'      # optional, exact string match on request body
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

### Validation Rules

- `request.uri` is required
- Must have exactly one of `response` (singular) or `responses` (plural)
- `body` and `body_file` are mutually exclusive (in both singular and plural forms)
- `sequence` is only valid with `responses` (plural), must be `cycle` or `once`
- Regex URIs (`~/` prefix) must compile as valid Go regexps

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
- Gzip-compressed upstream responses are decompressed for readability in captured coats
- Redirect responses are captured as-is (client does not follow redirects and returns the 3xx response as-is via `http.ErrUseLastResponse`)
- To proxy to upstreams with TLS certificates using negative serial numbers
  (rejected by Go 1.23+), set the environment variable
  `GODEBUG=x509negativeserial=1` before starting the proxy. See
  https://go.dev/doc/godebug#x509negativeserial for details.

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

Available options:
- `WithCoat(Coat)` — add a single coat
- `WithCoats(...Coat)` — add multiple coats
- `WithCoatFile(path)` — load coats from a YAML/JSON file
- `WithVerbose()` — enable verbose request logging

## Testing Expectations

- Unit tests for matcher: exact, glob, regex URI; method+ANY; header globs; query matching; precedence
- Unit tests for coat parsing/validation (YAML, JSON, mutual exclusivity rules)
- Integration tests for serve mode (start server, send requests, assert responses)
- Integration tests for proxy mode (proxy through, assert captured coat files)
- Tests for response sequences (cycle and once modes)
- Tests for hot-reload (modify coat file on disk, verify server picks up changes)
- Tests for TLS (self-signed cert)
- Tests for the public API (`trenchcoat_test.go`)
- Tests for CLI commands (`commands_test.go`)

See `docs/test-coverage-analysis.md` for detailed coverage data and test inventory.

## CI

GitHub Actions workflow at `.github/workflows/ci.yaml` runs:
- **Test**: `go test -v -count=1 -race -coverprofile=coverage.out` (uploads coverage artifact)
- **Lint**: golangci-lint v2.10.1 via `golangci-lint-action`
- **Vet**: `go vet`, `go mod tidy` check, `govulncheck`
- **Format**: `gofmt -l`, `goimports -l` (fail if any files are unformatted)
- **Build**: Cross-compile linux/darwin/windows x amd64/arm64 with ldflags (depends on all other jobs)

Releases are configured via `.goreleaser.yaml` (tar.gz archives with checksums).

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
- Support Linux, macOS, and Windows
- Coat files must be human-readable and hand-editable
- `body_file` paths resolve relative to the coat file's location
- Graceful shutdown on SIGINT/SIGTERM with 10s drain timeout
- Use `net.Listen("tcp4", addr)` for IPv4-only binding
- `sync.WaitGroup.Go()` (Go 1.25) for fire-and-forget goroutines
