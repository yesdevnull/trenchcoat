# Trenchcoat

Extensible mock, and proxy-to-mock, HTTP server written in Go.
Module path: `github.com/yesdevnull/trenchcoat`

## Repository Structure

```
cmd/trenchcoat/           CLI entrypoint (cobra: serve, proxy, validate)
internal/
  coat/                   Types, parsing, validation for coat files
  config/                 Viper-based config file loading
  matcher/                Request matching engine (exact, glob, regex URI)
  proxy/                  Proxy capture server
  server/                 Mock HTTP server, body_file resolution
examples/go-tests/        Example test suite using the programmatic API
docs/                     Demo walkthrough, roadmap, test coverage analysis
trenchcoat.go             Public API for Go test integration
coatfile.schema.json      JSON Schema (draft 2020-12) for coat file validation
```

Test files follow Go convention (`*_test.go` alongside source).

## Development

**Requirements:** Go 1.25.x (CI pins 1.25.7), golangci-lint v2.10.1+

If Go 1.25+ is not installed or `GOTOOLCHAIN` auto-download fails, install manually:

```bash
curl -fSL -o /tmp/go1.25.7.linux-amd64.tar.gz "https://go.dev/dl/go1.25.7.linux-amd64.tar.gz"
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go1.25.7.linux-amd64.tar.gz
```

### Commands

```bash
make build          # Build binary
make test           # Run tests (verbose, race detector)
make coverage       # Run tests with coverage, generate HTML report
make lint           # Run golangci-lint
make clean          # Clean build artifacts and test cache
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

Every feature begins with a test. Do not write implementation code without a corresponding failing test first.

1. **Red** — Write a failing test that defines expected behaviour
2. **Green** — Write the minimum code to pass
3. **Refactor** — Clean up while keeping tests green

## Architecture

### CLI Commands

The CLI uses cobra with three subcommands. All support `--config` for explicit config file path.

**`trenchcoat serve`** — `--coats`, `--port` (8080), `--tls-cert`/`--tls-key`, `--watch`, `--verbose`, `--log-format` (text|json)

**`trenchcoat proxy <upstream-url>`** — `--port` (8080), `--write-dir` (.), `--filter`, `--strip-headers` (Authorization/Cookie/Set-Cookie), `--no-headers`, `--capture-body` (true), `--dedupe` (overwrite|skip|append), `--verbose`, `--log-format`

**`trenchcoat validate <path>...`** — Validate coat files for schema correctness

Config discovery: `--config` flag > `.trenchcoat.yaml` in cwd > `~/.config/trenchcoat/config.yaml`. No config file required.

### Coat Specification

Coats are YAML or JSON files defining request/response mocks (`coatfile.schema.json` provides machine-readable validation):

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

**URI matching:** Exact (plain string) > Glob (`*`/`?`) > Regex (`~/` prefix)

**Match precedence** (highest to lowest): exact URI + most qualifiers (headers/query/body) > glob (longer literal prefix wins) > regex (definition order) > `method: ANY` ranks below method-specific > earlier definition wins as tiebreaker. Body matching capped at 1 MiB.

**Validation rules:**
- `request.uri` required; `response` xor `responses` required
- `body` and `body_file` mutually exclusive; `sequence` only valid with `responses` (`cycle`|`once`)
- Regex URIs must compile as valid Go regexps

### Proxy Capture

- File naming: `{METHOD}_{sanitised_path}_{status}.yaml`; dedupe via `overwrite`/`skip`/`append`
- Gzip responses decompressed for readability; redirects captured as-is (`http.ErrUseLastResponse`)
- Respects `http_proxy`/`https_proxy`/`no_proxy` env vars
- For upstreams with negative TLS serial numbers (Go 1.23+), set `GODEBUG=x509negativeserial=1`

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

Options: `WithCoat(Coat)`, `WithCoats(...Coat)`, `WithCoatFile(path)`, `WithVerbose()`
Helpers: `StringPtr(s string) *string` — convenience for setting `Request.Body`

## CI

GitHub Actions (`.github/workflows/ci.yaml`): **Test** (race, coverage) → **Lint** (golangci-lint v2.10.1) → **Vet** (`go vet`, `go mod tidy`, `govulncheck`) → **Format** (`gofmt`, `goimports`) → **Build** (linux/darwin/windows × amd64/arm64). Releases via `.goreleaser.yaml`.

## Pre-commit Requirements

Before every commit, run the following and fix any issues:

```bash
gofmt -w .                  # Format all Go code
goimports -w .              # Fix imports
golangci-lint run ./...     # Lint
go vet ./...                # Static analysis
go test -race ./...         # Run tests with race detector
```

All Go source files **must** be formatted with `gofmt` and `goimports` before committing.

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
