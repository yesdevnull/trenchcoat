# Trenchcoat

**Extensible mock, and proxy-to-mock, HTTP server written in Go.**

## Purpose

Trenchcoat is a CLI tool that serves mock HTTP responses based on configurable request/response definitions called "coats". It has two primary modes of operation:

1. **Serve mode** — acts as a mock HTTP server, matching incoming requests against loaded coats and returning the defined responses.
2. **Proxy mode** — acts as an HTTP proxy, forwarding requests to their intended destination, capturing both the request and response, and writing them as coat files for future use as mocks.

Trenchcoat is designed for use in enterprise environments and must handle corporate proxy configurations and PKI certificate chains.

## Constraints

- Written in Go, distributed as a single static binary.
- Must support Linux and macOS.
- No external runtime dependencies.
- Coat files must be human-readable and hand-editable (YAML and JSON).
- Logging should use the standard library `slog` package, which natively supports both structured text and JSON output. Default format is text.

---

## CLI Interface

```
trenchcoat serve [flags]
trenchcoat proxy <upstream-url> [flags]
trenchcoat validate <path>...
```

### `trenchcoat serve`

Start the mock HTTP server.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--coats` | `[]string` | `[]` | One or more paths to coat files to load. Accepts files and directories (non-recursive, `*.yaml`, `*.yml`, `*.json`). |
| `--port` | `int` | `8080` | Port to listen on. |
| `--tls-cert` | `string` | `""` | Path to TLS certificate file (PEM). Enables HTTPS. |
| `--tls-key` | `string` | `""` | Path to TLS private key file (PEM). Required if `--tls-cert` is set. |
| `--tls-ca` | `string` | `""` | Path to CA certificate chain file (PEM) for corporate PKI environments. Appended to the trust store for upstream connections. |
| `--watch` | `bool` | `false` | Watch coat files for changes and hot-reload without restarting. |
| `--verbose` | `bool` | `false` | Log each incoming request, match result, and matched coat name. |
| `--log-format` | `string` | `text` | Log output format: `text` or `json`. |

### `trenchcoat proxy`

Start in proxy capture mode. Forwards all requests to their destination and captures request/response pairs as coat files.

| Flag | Type | Default | Description |
|---|---|---|---|
| `<upstream-url>` | `string` | *(positional, required)* | The upstream base URL to proxy requests to. |
| `--port` | `int` | `8080` | Port to listen on. |
| `--write-dir` | `string` | `.` | Directory to write captured coat files to. Created if it does not exist. |
| `--filter` | `string` | `""` | Only capture requests whose URI matches this glob pattern (e.g. `/api/*`). Empty means capture all. |
| `--strip-headers` | `[]string` | `Authorization,Cookie,Set-Cookie` | Response/request headers to redact from captured coat files. Comma-separated. Set to empty string to disable. |
| `--dedupe` | `string` | `overwrite` | Deduplication strategy for repeated request/response pairs: `overwrite`, `skip`, or `append`. |
| `--tls-cert` | `string` | `""` | Path to TLS certificate file (PEM). Enables HTTPS on the proxy listener. |
| `--tls-key` | `string` | `""` | Path to TLS private key file (PEM). |
| `--tls-ca` | `string` | `""` | Path to CA certificate chain file (PEM). Used both for the proxy listener trust and for upstream connections. |
| `--verbose` | `bool` | `false` | Log each proxied request and capture event. |
| `--log-format` | `string` | `text` | Log output format: `text` or `json`. |

### `trenchcoat validate`

Validate one or more coat files for schema correctness without starting a server.

| Flag | Type | Default | Description |
|---|---|---|---|
| `<path>...` | `[]string` | *(positional, required)* | One or more coat file or directory paths to validate. |

Exit code 0 if all valid, non-zero with diagnostic output if any errors are found.

---

## Configuration File

Trenchcoat supports an optional configuration file to avoid repetitive flag usage. Configuration is loaded in the following precedence order (highest to lowest):

1. CLI flags (always win).
2. Configuration file.
3. Built-in defaults.

### File Discovery

Trenchcoat looks for a configuration file in the following order:

1. Path specified by `--config` flag.
2. `.trenchcoat.yaml` or `.trenchcoat.yml` in the current working directory.
3. `~/.config/trenchcoat/config.yaml`.

If no configuration file is found, Trenchcoat proceeds with defaults and CLI flags only. This is not an error.

### Supported Fields

```yaml
# .trenchcoat.yaml
port: 8080
log_format: text
coats:
  - ./mocks/api.yaml
  - ./mocks/auth.yaml
watch: true

tls:
  cert: ./certs/server.pem
  key: ./certs/server-key.pem
  ca: ./certs/corporate-ca-chain.pem

proxy:
  write_dir: ./captured
  strip_headers:
    - Authorization
    - Cookie
    - Set-Cookie
  dedupe: overwrite
  filter: "/api/*"
```

Viper handles the merge of config file and CLI flags natively.

---

When in proxy mode, Trenchcoat must respect the following environment variables for upstream connections:

- `http_proxy` / `HTTP_PROXY`
- `https_proxy` / `HTTPS_PROXY`
- `no_proxy` / `NO_PROXY`

Both lowercase and uppercase variants must be checked. Standard Go `net/http` proxy resolution via `http.ProxyFromEnvironment` handles this, but verify behaviour and document it.

---

## TLS / Corporate PKI

Trenchcoat must support enterprise PKI environments where traffic is intercepted by corporate proxies with custom CA certificates.

- `--tls-cert` and `--tls-key` enable HTTPS on the Trenchcoat listener itself.
- `--tls-ca` provides a CA certificate chain (PEM bundle) that is appended to the system trust store. This is used for:
  - Proxy mode upstream connections through corporate MITM proxies.
  - Any future features that make outbound connections.
- If `--tls-ca` is provided but the file cannot be parsed, Trenchcoat must exit with a clear error message.

---

## Coat Specification

A coat is an individual request/response mock definition. Coat files contain one or more coats and can be written in YAML or JSON.

### Schema

```yaml
coats:
  - name: "descriptive-name"          # optional — used in logging and validation output
    request:
      method: GET                      # optional, default: GET. Supports any HTTP method or ANY to match all.
      uri: "/api/v1/users"             # mandatory. Supports exact match, glob (*/?)  and regex (~/).
      headers:                         # optional. Subset match — request must contain these headers.
        Accept: "application/json"
        Authorization: "Bearer *"      # glob matching on header values
      query:                           # optional. Either a raw query string or a map of key/value pairs.
        page: "1"
        limit: "*"                     # glob matching on query values
      # Alternatively, query can be a plain string:
      # query: "page=1&limit=10"

    response:
      code: 200                        # optional, default: 200
      headers:                         # optional. Response headers to return.
        Content-Type: "application/json"
        X-Request-Id: "mock-12345"
      body: |                          # optional. Inline response body. Mutually exclusive with body_file.
        {"users": [{"id": 1, "name": "Alice"}]}
      body_file: "./fixtures/users.json"  # optional. Source response body from file path (relative to coat file location).
      delay_ms: 0                      # optional, default: 0. Artificial response delay in milliseconds.

    # Stateful sequence support (optional).
    # If present, 'responses' (plural) is used instead of 'response' (singular).
    # Mutually exclusive with 'response'.
    responses:
      - code: 503
        body: "Service Unavailable"
      - code: 503
        body: "Service Unavailable"
      - code: 200
        headers:
          Content-Type: "application/json"
        body: '{"status": "ok"}'
    sequence: cycle                    # optional, default: cycle. Options: cycle (loop), once (404 after exhaustion).
```

### Validation Rules

- A coat must have either `response` (singular) or `responses` (plural), never both. If both are present, validation must fail with a clear error.
- `body` and `body_file` within a response are mutually exclusive. If both are present, validation must fail.
- `uri` is the only mandatory field on `request`.
- `body_file` paths are resolved relative to the coat file's location, not the working directory.
- `query` accepts either a plain string (e.g. `"page=1&limit=10"`) or a map of key/value string pairs. When provided as a map, values support glob matching. When provided as a string, it is matched literally against the full query string.
- `sequence` is only valid when `responses` (plural) is used. Its presence alongside singular `response` must fail validation.

### File Format Detection

Coat file format is determined by file extension:

| Extension | Format |
|---|---|
| `.yaml`, `.yml` | YAML |
| `.json` | JSON |

Files with unrecognised extensions are skipped with a warning. Both formats use the same schema — only the serialisation differs.

### URI Matching

URIs support three matching modes, determined by the pattern syntax:

| Mode | Syntax | Example | Matches |
|---|---|---|---|
| Exact | Plain string | `/api/v1/users` | Only `/api/v1/users` |
| Glob | Contains `*` or `?` | `/api/v1/users/*` | `/api/v1/users/123`, `/api/v1/users/abc` |
| Regex | Prefixed with `~/` | `~/api/v1/users/\d+` | `/api/v1/users/123` but not `/api/v1/users/abc` |

### Request Matching Precedence

When multiple coats could match an incoming request, the most specific match wins. Specificity is determined by the following priority order (highest to lowest):

1. Exact URI match with method + headers + query matching.
2. Exact URI match with method + fewer qualifiers.
3. Glob URI match, ordered by specificity (longer literal prefix wins).
4. Regex URI match, in file-definition order.
5. `method: ANY` matches are ranked below same-specificity method-specific matches.

If two coats have identical specificity, the first one defined (in file order, files in lexicographic order) wins.

When a request matches a coat, the coat name (or a generated identifier if unnamed) and the match reason should be logged at `--verbose` level.

When no coat matches, respond with `404 Not Found` and a JSON body: `{"error": "no matching coat", "method": "GET", "uri": "/the/requested/path"}`.

---

## Proxy Capture Flow

1. Client sends an HTTP request to Trenchcoat's proxy listener.
2. Trenchcoat captures the full request (method, URI, headers, query parameters, body).
3. If `--filter` is set and the request URI does not match the glob pattern, the request is proxied without capture.
4. Trenchcoat forwards the request to the upstream destination, respecting proxy environment variables and the `--tls-ca` trust chain.
5. Trenchcoat receives the upstream response and captures the full response (status code, headers, body).
6. The response is relayed back to the client, completing the request/response cycle.
7. Headers listed in `--strip-headers` are removed from both the captured request and response.
8. The captured request/response pair is written as a coat file to `--write-dir`.

### Captured File Naming Convention

Files are named using the pattern:

```
{METHOD}_{sanitised_path}_{status_code}_{unix_timestamp}.yaml
```

Example: `GET_api_v1_users_200_1719384000.yaml`

Path sanitisation rules:
- Leading `/` is stripped.
- All remaining `/` are replaced with `_`.
- Query strings are excluded from the filename.
- Non-alphanumeric characters (other than `_` and `-`) are stripped.

### Deduplication

Controlled by `--dedupe`:

| Strategy | Behaviour |
|---|---|
| `overwrite` | Replace existing file if same method + URI + status code combination exists. Timestamp is updated. |
| `skip` | Do not write if a file with the same method + URI + status code combination already exists. |
| `append` | Always write a new file, appending an incrementing counter before the timestamp: `GET_api_v1_users_200_2_1719384000.yaml`. |

---

## Request Logging

When `--verbose` is enabled, every incoming request should be logged with:

- Timestamp
- HTTP method and full URI (including query string)
- Whether a coat match was found
- The matched coat's `name` (or generated identifier)
- Response status code returned
- Response time (including any artificial delay)

In proxy mode, additionally log:
- Upstream response time
- Whether the request was captured or filtered out

---

## Stateful Response Sequences

Coats using `responses` (plural) maintain an internal counter per coat, tracking which response to serve next.

- **`cycle` mode** (default): After the last response is served, the counter resets to the first response.
- **`once` mode**: After the last response is served, all subsequent requests to that coat return `404 Not Found` with body `{"error": "sequence exhausted", "coat": "coat-name"}`.

The sequence counter is held in memory and resets on server restart. If `--watch` triggers a reload of a coat file containing sequences, the counter for that coat resets.

---

## Error Handling

- If a coat file cannot be parsed, log the error with the file path and line number (if available), and skip that file. Do not exit — serve with the remaining valid coats and log a warning at startup.
- If `body_file` references a file that does not exist, log a warning at startup. If that coat is matched at request time, respond with `500 Internal Server Error` and body `{"error": "body_file not found", "path": "the/missing/file.json"}`.
- If `--tls-cert` is provided without `--tls-key` (or vice versa), exit with a clear error message.
- If the listen port is already in use, exit with a clear error message.

## Graceful Shutdown

`trenchcoat serve` and `trenchcoat proxy` must handle `SIGINT` and `SIGTERM` signals gracefully:

1. Stop accepting new connections.
2. Drain in-flight requests (with a reasonable timeout, e.g. 10 seconds).
3. In proxy mode, flush any pending coat file writes to disk.
4. Exit with code 0.

If the drain timeout is exceeded, log a warning listing any in-flight requests and exit with code 1. This behaviour is important for CI environments that send `SIGTERM` on pipeline timeout.

---



Use Red/Green/Refactor TDD throughout development:

1. **Red** — Write a failing test that defines the expected behaviour.
2. **Green** — Write the minimum code to make the test pass.
3. **Refactor** — Clean up the implementation while keeping tests green.

Every feature should begin with a test. Do not write implementation code without a corresponding failing test first.

## Testing Requirements

- Unit tests for the request matching engine covering: exact, glob, and regex URI matching; method matching including `ANY`; header subset matching with globs; query parameter matching (both string and map forms); precedence ordering.
- Unit tests for coat file parsing and validation (YAML and JSON), including all mutual exclusivity rules.
- Integration tests for serve mode: start the server, send HTTP requests, assert responses.
- Integration tests for proxy mode: start the proxy, send requests through it, assert captured coat file contents.
- Integration tests for response sequences in both `cycle` and `once` modes.
- Tests for hot-reload (`--watch`): modify a coat file on disk and verify the server picks up changes.
- Tests for TLS: serve with a self-signed cert, make requests trusting that cert.

---

## Build & Distribution

- Use standard `go build`.
- Provide a `Makefile` with targets: `build`, `test`, `lint`, `clean`.
- Use `goreleaser` configuration for cross-platform release builds (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64).
- Embed version information at build time via `-ldflags` (git tag, commit hash, build date).
- Binary name: `trenchcoat`.

---

## Dependencies

Prefer the standard library where practical. Suggested dependencies:

| Dependency | Purpose |
|---|---|
| `cobra` | CLI framework and subcommand routing |
| `viper` | Configuration and flag binding |
| `fsnotify` | File watching for `--watch` hot-reload |
| `slog` (stdlib) | Structured logging — text and JSON output formats |
| `gopkg.in/yaml.v3` | YAML coat file parsing |

Avoid heavy frameworks. No web frameworks — use `net/http` directly.

---

## Documentation: Go Test Integration

Include documentation and a working demo showing how to use Trenchcoat as a mock server within Go test suites. This is a key adoption driver — users need to see how to spin up Trenchcoat programmatically in their tests rather than only as a standalone CLI process.

### Requirements

- Provide a `trenchcoat` Go package that can be imported and used directly in tests, exposing a programmatic API for starting/stopping the server and loading coats.
- The package should allow starting a server on an ephemeral port (`:0`) so tests can run in parallel without port conflicts.
- The server's base URL should be easily retrievable for use in test HTTP clients.
- Coat definitions should be loadable from both files and inline Go structs/YAML strings, so simple tests don't need external fixture files.

### Demo

Include a `examples/go-tests/` directory in the repository containing a self-contained, runnable demo. The demo should be a single Go test file (`example_test.go`) that demonstrates:

1. **Basic mock** — a single coat serving a JSON response, with a test making an HTTP request and asserting the response body and status code.
2. **Multiple coats** — several coats loaded together, with separate test functions hitting different endpoints and verifying each returns the correct mock response.
3. **Method differentiation** — a `GET` and `POST` to the same URI returning different responses.
4. **Header matching** — a coat that only matches when a specific header is present, with tests for both the matching and non-matching cases.
5. **Response sequences** — a coat using `responses` (plural) with `cycle` mode, with a test that makes multiple requests and asserts the rotating responses.
6. **Glob URI matching** — a coat with a wildcard URI, with tests showing it matches various paths.

Each test should be clearly commented explaining what it demonstrates. The file should serve as a copy-paste starting point for users writing their own test suites.

### Example skeleton

The demo should follow this general pattern:

```go
func TestBasicMock(t *testing.T) {
    // Start a Trenchcoat server with an inline coat definition
    srv := trenchcoat.NewServer(
        trenchcoat.WithCoat(trenchcoat.Coat{
            Name: "get-users",
            Request: trenchcoat.Request{
                Method: "GET",
                URI:    "/api/v1/users",
            },
            Response: trenchcoat.Response{
                Code: 200,
                Headers: map[string]string{
                    "Content-Type": "application/json",
                },
                Body: `{"users": [{"id": 1, "name": "Alice"}]}`,
            },
        }),
    )
    srv.Start(t)        // starts on ephemeral port, registers t.Cleanup for shutdown
    defer srv.Stop()

    // Make a request to the mock server
    resp, err := http.Get(srv.URL + "/api/v1/users")
    // ... assert response ...
}
```

This is illustrative — the final API design should be idiomatic Go and ergonomic for test authors. The key is that starting a mock server and loading coats should be achievable in a handful of lines.

---

The following features are out of scope for the initial build but should be considered in the architecture to avoid painful refactors later:

- **Passthrough mode** — a hybrid of serve and proxy. Serve matched coats as mocks, but forward unmatched requests to a real upstream URL. This will likely become a `--passthrough <upstream-url>` flag on the `serve` subcommand.
- **Complex directory structure** — support recursive directory loading with organisational conventions (e.g. `mocks/users/list.yaml`, `mocks/auth/login.yaml`) and potential shared default headers/config at directory level.
- **Request body matching** — allow coats to match on request body content (e.g. substring contains, JSONPath matching) for POST/PUT/PATCH requests. This would add a `body` field to the `request` schema and extend the matching engine and precedence rules accordingly.

When implementing the matching engine, request router, and coat loader, keep these features in mind so that the internal interfaces can accommodate them without major restructuring.
