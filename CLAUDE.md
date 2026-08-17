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
  httputil/               Shared HTTP helpers
    body.go               ReconstitutedBody: replay a consumed body for downstream readers
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
coatfile.schema.json      JSON Schema for coat files (hand-maintained — see Validation Rules)
.claude/
  settings.json           Checked-in Claude Code config (PostToolUse hooks)
  hooks/
    gofmt-on-write.py     Formats each Go file as it is edited
    coat-schema-sync.py   Warns when coat types drift from coatfile.schema.json
    test_hooks.py         Tests for both hooks
.github/workflows/ci.yaml  CI pipeline (test, lint, vet, format, build)
.goreleaser.yaml          GoReleaser config for cross-platform releases
renovate.json             Renovate dependency auto-update config
```

## Development

### Requirements

- Go 1.25.x. The exact patch release lives in the `toolchain` directive in
  `go.mod`; CI installs it via `go-version-file: go.mod`, and Renovate bumps it.
  The `go` directive stays at `1.25` as the minimum for consumers of the package.
- golangci-lint. The version is pinned in `.github/workflows/ci.yaml`.

### Installing Go

If Go 1.25+ is not installed or the auto-download via `GOTOOLCHAIN` fails (e.g.
due to DNS/network restrictions), install manually:

```bash
# Download (linux/amd64 — adjust for your platform)
curl -fSL -o /tmp/go1.25.13.linux-amd64.tar.gz "https://go.dev/dl/go1.25.13.linux-amd64.tar.gz"

# Install (removes any previous Go installation in /usr/local/go)
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go1.25.13.linux-amd64.tar.gz

# Verify
go version   # should print "go version go1.25.13 linux/amd64"
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

### Claude Code Hooks

`.claude/settings.json` is checked in and registers two `PostToolUse` hooks that
fire after Claude edits a file. Both are deliberately silent unless they have
something to say.

| Hook | Fires on | Does |
|------|----------|------|
| `gofmt-on-write.py` | any `.go` file | Runs `gofmt -w` then `goimports -w` on that one file |
| `coat-schema-sync.py` | `internal/coat/types.go`, `internal/coat/validate.go` | Warns if `coatfile.schema.json` is not also dirty in the working tree |

The formatter only sees files **Claude** edits — your own editor changes are not
covered, so the pre-commit checks below still stand. It is a safety net for the
CI Format job, not a replacement for the manual pass.

The schema hook exists because `coatfile.schema.json` hand-mirrors the coat
schema and nothing enforces that they agree; the warning clears as soon as the
schema is touched. See the Validation Rules section.

Both scripts are plain stdlib Python with no dependencies, and must stay
runnable under **Python 3.9** — macOS ships 3.9.6 as `/usr/bin/python3`, and the
`#!/usr/bin/env python3` shebang resolves to whatever is first on `PATH`. That
rules out PEP 604 (`str | None`) annotations unless
`from __future__ import annotations` is present.

```bash
python3 .claude/hooks/test_hooks.py     # Test both hooks
```

The tests drive the real hook scripts against real `gofmt`, `goimports` and
`git` in throwaway repos. Nothing is mocked — the point of the hooks is that
they agree with the tools CI runs.

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
- `--pretty-json` — Pretty-print JSON response bodies in captured coat files
- `--body-file-threshold` — Write response bodies larger than N bytes to separate files (0 = always inline)
- `--name-template` — Custom template for captured coat file names (e.g. `{{.Method}}-{{.Path}}-{{.Status}}`)
- `--tls-server-name` — Verify the upstream TLS certificate against this hostname instead of the upstream URL host (also sets SNI)
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
      uri: "/api/v1/users"          # mandatory. Exact, glob (*/?/**), or regex (~/).
      headers:                       # optional, subset match with glob values
        Authorization: "Bearer *"
      query:                         # optional, string or map with glob values
        page: "1"
      body: '{"name": "alice"}'      # optional, exact string match on request body
      body_match: exact              # optional: exact (default), glob, contains, regex
    response:
      code: 200
      headers:
        Content-Type: "application/json"
      body: '{"users": []}'         # or body_file: "./fixtures/data.json"
      delay_ms: 0
      delay_jitter_ms: 0            # random delay added to delay_ms, in [0, N) ms
    # OR for sequences (mutually exclusive with response):
    responses:
      - code: 503
        body: "unavailable"
      - code: 200
        body: "ok"
    sequence: cycle                  # cycle (default) or once
```

### Response Body Templating

Response bodies containing `{{` are rendered as a Go `text/template` with request
context, after `body_file` resolution — so `body_file` contents are templated too.
Available fields and methods:

| Field            | Meaning                                              |
|------------------|------------------------------------------------------|
| `.Method`        | Request method                                       |
| `.Path`          | Request URL path                                     |
| `.Body`          | Request body (capped at 1 MiB, `maxRecordBodySize`)  |
| `.Query "page"`  | First value of a query parameter, `""` if absent     |
| `.Segment 3`     | Nth path segment, 0-indexed from root, `""` if absent|

A body that legitimately contains `{{` will be treated as a template. Parse
failures return the body unrendered and silently; execution failures log a
warning and return the body unrendered.

### URI Matching Modes

| Mode  | Syntax            | Example                    |
|-------|-------------------|----------------------------|
| Exact | Plain string      | `/api/v1/users`            |
| Glob  | Contains `*`, `?`, `[` or `**` | `/api/v1/users/*` |
| Glob  | `**` multi-segment | `/api/**/posts/*`         |
| Regex | `~/` prefix       | `~/api/v1/users/\d+`       |

Regex URIs are anchored as a group — `^(?:pattern)$` — so a top-level
alternation is bounded as a whole: `~/users|/accounts` matches `/users` and
`/accounts`, and neither `/users/extra` nor `/x/y/accounts`.

A URI is treated as a glob when it contains `*`, `?` or `[`. `[` is included
because the underlying matcher is `doublestar`, so `/items[abc]` is a character
class rather than a literal path. To match a literal bracket, escape it with a
backslash — `/items\[abc\]` — noting that the URI still counts as a glob and
is matched as one; there is no way to make a bracketed URI an exact match.

### Header, Query and Body Globs

Header values, query values and `body_match: glob` patterns use a *different*
glob dialect from URIs, because these values are not paths:

| Metacharacter | Matches                                        |
|---------------|------------------------------------------------|
| `*`           | any sequence of characters, `/` and `\n` included |
| `?`           | any single character                           |

Everything else, `[` included, matches literally. So `Content-Type: "*"` matches
`application/json`, and `redirect: "*"` matches `/home/dash` — unlike URI globs,
where `*` stops at a path segment boundary.

### Match Precedence (highest to lowest)

1. Exact URI + method + headers + query
2. Exact URI + method + fewer qualifiers
3. Glob URI (longer literal prefix wins)
4. Regex URI (file-definition order)
5. `method: ANY` ranks below method-specific at same specificity

Specificity is the count of qualifiers on the request: headers, query fields, and
body presence.

### Unmatched Requests

Requests that match no coat, and requests hitting an exhausted `once` sequence,
both return **404** with a JSON body (`{"error": ...}`).

A request that *matches* a coat defining neither `response` nor `responses`
returns **500** naming the coat, and logs at `ERROR`: that is a fault in the
coat rather than a request that found no match. Validation and `WithCoatFile`
both reject such a coat, so this is only reachable via `WithCoat`/`WithCoats`.

### Validation Rules

- Coat files are parsed **strictly**: an unknown YAML or JSON key is a parse
  error naming the field, not a silently ignored one. Top-level keys beginning
  `x-` are the one exception, so a file can hold a YAML anchor for coats to
  merge in
- A coat file must contain exactly one document, in either format; anything
  after it is an error. A YAML file may still carry the markers of a single
  document — a leading `---`, a trailing `---` or `...` — but a second `---`
  document is rejected rather than silently ignored
- A URI containing `*`, `?` or `[` must be a valid `doublestar` pattern
- `request.uri` is required
- Must have exactly one of `response` (singular) or `responses` (plural)
- `body` and `body_file` are mutually exclusive (in both singular and plural forms)
- `sequence` is only valid with `responses` (plural), must be `cycle` or `once`
- Regex URIs (`~/` prefix) must compile as valid Go regexps
- `body_match` must be `exact`, `glob`, `contains` or `regex`, and requires
  `request.body` to be set; with `regex`, the body must compile as a Go regexp
- `body_file` must be a relative path with no `..` components
- `delay_ms` and `delay_jitter_ms` must be non-negative, and combined must not
  exceed `coat.MaxDelayMs` (60000)

`ValidateWithWarnings` also returns non-fatal **warnings** alongside errors:
duplicate coat names, and regex URIs simple enough to be expressed as globs.
`Validate` returns the errors only.

`coatfile.schema.json` duplicates this schema for editor completion. Nothing in
the build or test suite enforces that it stays in sync — when you add or change
a coat field, update it by hand in the same commit. The `coat-schema-sync.py`
hook warns when you forget.

### Key Dependencies

| Package         | Purpose                        |
|-----------------|--------------------------------|
| cobra           | CLI framework                  |
| viper           | Config file and flag binding   |
| fsnotify        | Hot-reload file watching       |
| doublestar/v4   | Glob matching with `**` support|
| slog (stdlib)   | Structured logging             |
| gopkg.in/yaml.v3 | YAML parsing                 |

### Proxy Capture

- Respects `http_proxy`/`https_proxy`/`no_proxy` env vars
- File naming: `{METHOD}_{sanitised_path}_{status}.yaml`
- Dedupe strategies: `overwrite` (stable filename), `skip`, `append`
- Headers in `--strip-headers` are redacted from captures
- Every request header a coat records becomes a **match constraint at replay**,
  so client- and connection-specific headers are never captured: hop-by-hop
  headers, `Content-Length`, `Host`, `User-Agent`, `Accept`, `Accept-Encoding`
  and `Accept-Language`. Recording them would tie a coat to the tool that
  captured it — a capture taken with curl would 404 for every other client.
  `Content-Type` and custom headers such as `X-Api-Key` are still captured.
- Headers a peer scopes to one hop by naming them in its `Connection` header
  are withheld from the wire in both directions, and are not captured either —
  a coat recording them would demand, at replay, a header the upstream never
  saw
- Captured `request.uri` is the **decoded** path, so `/files/dir%2Ffile` and
  `/files/dir/file` produce one coat even though they are different requests
  upstream; with `--dedupe skip` the second is discarded. Forwarding preserves
  the encoding — it is only the capture that collapses
- `Content-Length` is never recorded in a captured **response** either: the
  captured body differs from the upstream body whenever it is pretty-printed or
  decompressed, and `net/http` derives the correct length when serving
- Gzip-compressed upstream responses are decompressed for readability in captured coats
- Redirect responses are captured as-is (client does not follow redirects and returns the 3xx response as-is via `http.ErrUseLastResponse`)
- To proxy to an upstream whose TLS certificate is issued for a different
  hostname than the address it is served from, pass `--tls-server-name` with the
  hostname the certificate covers. This sets `tls.Config.ServerName`, so it
  becomes both the SNI name sent upstream and the name the certificate is
  verified against. Chain and expiry verification remain enabled.
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

`Request.Body` is a `*string` so an empty body can be distinguished from no body
constraint — use `trenchcoat.StringPtr("...")` to set it.

Available options:
- `WithCoat(Coat)` — add a single coat
- `WithCoats(...Coat)` — add multiple coats
- `WithCoatFile(path)` — load coats from a YAML/JSON file
- `WithVerbose()` — enable verbose request logging
- `WithTLS(certFile, keyFile)` — use TLS with explicit certificate
- `WithSelfSignedTLS()` — auto-generate self-signed cert; sets `srv.TLSClient`

Assertion methods (available after `Start`):
- `srv.AssertCalled(t, "name")` — coat called at least once
- `srv.AssertCalledN(t, "name", n)` — called exactly N times
- `srv.AssertNotCalled(t, "name")` — never called
- `srv.Requests("name")` — return captured requests
- `srv.ResetCalls()` — clear call data

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
- **Lint**: golangci-lint via `golangci-lint-action`
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

The `gofmt-on-write.py` hook already formats files Claude edits, but it does not
see edits made any other way — run the above regardless.

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
