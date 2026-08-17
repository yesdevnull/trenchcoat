# Trenchcoat

Extensible mock, and proxy-to-mock, HTTP server written in Go.

Trenchcoat serves mock HTTP responses based on configurable request/response definitions called "coats". It has two primary modes:

- **Serve** — acts as a mock HTTP server, matching incoming requests against loaded coats and returning defined responses.
- **Proxy** — acts as an HTTP proxy, forwarding requests to their destination, capturing request/response pairs, and writing them as coat files for future use as mocks.

It ships as a single static binary with no runtime dependencies and also provides a Go package for embedding mock servers directly in test suites.

## Installation

### Latest release

```sh
go install github.com/yesdevnull/trenchcoat/cmd/trenchcoat@latest
```

### Latest dev version

Install the latest commit on the `main` branch:

```sh
go install github.com/yesdevnull/trenchcoat/cmd/trenchcoat@main
```

## Quick start

Create a coat file `mocks/hello.yaml`:

```yaml
coats:
  - name: hello
    request:
      uri: "/hello"
    response:
      code: 200
      headers:
        Content-Type: application/json
      body: '{"message": "Hello, world!"}'
```

Start the mock server:

```sh
trenchcoat serve --coats mocks/
```

In another terminal:

```sh
curl http://localhost:8080/hello
# {"message": "Hello, world!"}
```

## CLI usage

`--config` is a global flag available on every subcommand (see [Configuration](#configuration)).

### `trenchcoat serve`

Start the mock HTTP server.

```
trenchcoat serve [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--coats` | `[]` | Paths to coat files or directories to load (non-recursive; `*.yaml`, `*.yml`, `*.json`). |
| `--port` | `8080` | Port to listen on. |
| `--tls-cert` | | Path to TLS certificate file (PEM). Enables HTTPS. Must be provided together with `--tls-key`. |
| `--tls-key` | | Path to TLS private key file (PEM). Must be provided together with `--tls-cert`. |
| `--watch` | `false` | Watch coat files for changes and hot-reload without restarting. |
| `--verbose` | `false` | Log each incoming request, match result, and matched coat name. |
| `--log-format` | `text` | Log output format: `text` or `json`. |

### `trenchcoat proxy`

Start in proxy capture mode. Forwards requests to an upstream and captures request/response pairs as coat files.

```
trenchcoat proxy <upstream-url> [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--port` | `8080` | Port to listen on. |
| `--write-dir` | `.` | Directory to write captured coat files to. Created if it doesn't exist. |
| `--filter` | | Only capture requests whose URI matches this glob (e.g. `/api/*`). Empty captures all. |
| `--strip-headers` | `Authorization,Cookie,Set-Cookie` | Headers to redact from captured coats. Set to empty string to disable. |
| `--no-headers` | `false` | Omit all headers from captured coat files. Mutually exclusive with `--strip-headers`. |
| `--capture-body` | `true` | Capture request body in coat files for any request with a body. |
| `--dedupe` | `overwrite` | Deduplication strategy: `overwrite`, `skip`, or `append`. |
| `--pretty-json` | `false` | Pretty-print JSON response bodies in captured coat files. |
| `--body-file-threshold` | `0` | Write response bodies larger than N bytes to separate files. `0` always inlines. |
| `--name-template` | | Custom template for captured coat file names (e.g. `{{.Method}}-{{.Path}}-{{.Status}}`). |
| `--tls-server-name` | | Verify the upstream TLS certificate against this hostname instead of the upstream URL host. Also sets SNI. |
| `--verbose` | `false` | Log each proxied request and capture event. |
| `--log-format` | `text` | Log output format: `text` or `json`. |

Captured file names are built from `{METHOD}_{sanitised_path}_{status_code}`, with a suffix that depends on `--dedupe`:

| Strategy | File name | Behaviour |
|---|---|---|
| `overwrite` | `{base}.yaml` | Stable name, so a repeated request replaces the earlier capture. |
| `skip` | `{base}_{unix_timestamp}.yaml` | Requests matching an existing capture are not written again. |
| `append` | `{base}_{unix_timestamp}.yaml`, then `{base}_{n}_{unix_timestamp}.yaml` | Every request is kept as a separate file. |

`--name-template` replaces the `{base}` portion. Rendered names are stripped of path separators and any character outside `[a-zA-Z0-9_-]`.

Behaviour worth knowing when capturing:

- Requests and responses larger than 10 MiB are rejected rather than captured.
- Gzipped upstream responses are decompressed before being written to the coat file, so captures stay readable. The client still receives the response as the upstream sent it.
- Redirects are not followed. The 3xx response is relayed and captured as-is.
- `http_proxy`, `https_proxy`, and `no_proxy` are respected for upstream connections.

### `trenchcoat validate`

Validate coat files for schema correctness without starting a server.

```
trenchcoat validate <path>...
```

Exits 0 if all files are valid, non-zero with diagnostics if any errors are found. Warnings are printed to stderr but do not cause a non-zero exit.

## Configuration

Trenchcoat supports an optional YAML configuration file to avoid repetitive flag usage. CLI flags always take precedence over config file values.

Config file discovery order:

1. Path specified by `--config`.
2. `.trenchcoat.yaml` or `.trenchcoat.yml` in the current working directory.
3. `~/.config/trenchcoat/config.yaml`.

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

proxy:
  write_dir: ./captured
  strip_headers:
    - Authorization
    - Cookie
    - Set-Cookie
  dedupe: overwrite
  filter: "/api/*"
  tls_server_name: api.internal.example.com
```

## Coat file format

Coat files define one or more request/response mock definitions in YAML or JSON. Format is determined by file extension (`.yaml`/`.yml` or `.json`).

```yaml
coats:
  - name: "get-users"                  # optional, used in logging
    request:
      method: GET                      # optional, default: GET (use ANY to match all methods)
      uri: "/api/v1/users"             # required — exact, glob (*, ?, **) or regex (~/)
      headers:                         # optional, subset match with glob support on values
        Accept: "application/json"
        Authorization: "Bearer *"
      query:                           # optional, map with glob values or raw query string
        page: "1"
        limit: "*"
      body: '{"name": "alice"}'        # optional, match against the request body
      body_match: exact                # optional: exact (default), glob, contains, regex

    response:
      code: 200                        # optional, default: 200
      headers:
        Content-Type: "application/json"
      body: |                          # inline body, mutually exclusive with body_file
        {"users": [{"id": 1, "name": "Alice"}]}
      # body_file: "./fixtures/users.json"  # load body from file (relative to coat file)
      delay_ms: 0                      # optional artificial delay in ms
      delay_jitter_ms: 0               # optional random extra delay in [0, N) ms
```

### URI matching modes

| Mode | Syntax | Example | Matches |
|---|---|---|---|
| Exact | Plain string | `/api/v1/users` | Only `/api/v1/users` |
| Glob | Contains `*` or `?` | `/api/v1/users/*` | `/api/v1/users/123`, `/api/v1/users/abc` |
| Glob | `**` spans path segments | `/api/**/posts/*` | `/api/v1/users/1/posts/9` |
| Regex | Prefixed with `~/` | `~/api/v1/users/\d+` | `/api/v1/users/123` but not `/api/v1/users/abc` |

When multiple coats match, they are ranked in this order:

1. URI mode — exact beats glob, glob beats regex.
2. Number of qualifiers — each `headers` entry, each `query` entry, and a `body` constraint adds one.
3. For globs only — the longer literal prefix wins.
4. Method — a specific method beats `ANY`.
5. Definition order — the first coat defined wins.

### Request body matching

The optional `body` field constrains a coat to requests with a matching body. When omitted, any body (or no body) matches. When set — even to an empty string — only requests whose body matches are selected. A coat with a `body` constraint is considered more specific than one without, so it wins when both otherwise tie.

`body_match` selects how `body` is compared, and requires `body` to be set:

| Mode | Comparison |
|---|---|
| `exact` (default) | The body equals `body` exactly. |
| `glob` | The body matches `body` as a glob pattern. |
| `contains` | The body contains `body` as a substring. |
| `regex` | The body matches `body` as a Go regular expression. Validated at load time. |

Glob matching on bodies, header values, and query values uses `path.Match` semantics, where `*` does not match `/`. Use `contains` or `regex` for bodies containing paths or URLs.

### Response sequences

Use `responses` (plural) instead of `response` (singular) to serve a stateful sequence of responses. The two forms are mutually exclusive.

```yaml
coats:
  - name: "flaky-health"
    request:
      uri: "/health"
    responses:
      - code: 503
        body: "Service Unavailable"
      - code: 503
        body: "Service Unavailable"
      - code: 200
        body: '{"status": "ok"}'
    sequence: cycle  # cycle (default) loops forever, once returns 404 after exhaustion
```

## Go test integration

Trenchcoat provides a Go package for spinning up mock servers directly in test suites. This is particularly useful in Terraform provider acceptance tests or any integration test that needs to mock an upstream HTTP API.

```sh
go get github.com/yesdevnull/trenchcoat
```

### Basic usage

```go
func TestMyAPI(t *testing.T) {
    srv := trenchcoat.NewServer(
        trenchcoat.WithCoat(trenchcoat.Coat{
            Name: "get-users",
            Request: trenchcoat.Request{
                Method: "GET",
                URI:    "/api/v1/users",
            },
            Response: &trenchcoat.Response{
                Code:    200,
                Headers: map[string]string{"Content-Type": "application/json"},
                Body:    `{"users": [{"id": 1, "name": "Alice"}]}`,
            },
        }),
    )
    srv.Start(t) // starts on an ephemeral port, registers t.Cleanup for shutdown

    resp, err := http.Get(srv.URL + "/api/v1/users")
    if err != nil {
        t.Fatal(err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
}
```

Key points:

- `srv.Start(t)` binds to `127.0.0.1:0` (ephemeral port), so tests run in parallel without port conflicts.
- `srv.URL` contains the base URL (e.g. `http://127.0.0.1:54321`) after `Start` is called.
- Cleanup is registered via `t.Cleanup`, so the server shuts down automatically when the test finishes. `srv.Stop()` is available for shutting down early and is safe to call more than once.

### Loading coats from files

```go
srv := trenchcoat.NewServer(
    trenchcoat.WithCoatFile("testdata/mocks.yaml"),
)
```

### Multiple inline coats

```go
srv := trenchcoat.NewServer(
    trenchcoat.WithCoats(
        trenchcoat.Coat{
            Name:     "list-users",
            Request:  trenchcoat.Request{Method: "GET", URI: "/api/users"},
            Response: &trenchcoat.Response{Code: 200, Body: `{"users": []}`},
        },
        trenchcoat.Coat{
            Name:     "create-user",
            Request:  trenchcoat.Request{Method: "POST", URI: "/api/users"},
            Response: &trenchcoat.Response{Code: 201, Body: `{"id": 2}`},
        },
    ),
)
```

### Server options

| Option | Description |
|---|---|
| `WithCoat(Coat)` | Add a single coat definition. |
| `WithCoats(...Coat)` | Add multiple coat definitions. |
| `WithCoatFile(path)` | Load coats from a YAML or JSON file. |
| `WithVerbose()` | Log each incoming request and match result. |
| `WithTLS(certFile, keyFile)` | Serve HTTPS using an explicit certificate. |
| `WithSelfSignedTLS()` | Serve HTTPS using a generated certificate, and set `srv.TLSClient` to a client that trusts it. |

`srv.TLSClient` is only guaranteed to be set by `WithSelfSignedTLS`; it may be nil with `WithTLS`.

### Asserting on received requests

The server records every request that matched a coat, keyed by the coat's `Name`, so give any coat you want to assert on a name. Recorded bodies are truncated past 1 MiB (the response the client receives is unaffected).

```go
srv.AssertCalled(t, "create-widget")        // called at least once
srv.AssertCalledN(t, "read-widget", 2)      // called exactly twice
srv.AssertNotCalled(t, "delete-widget")     // never called

for _, req := range srv.Requests("create-widget") {
    // req.Method, req.URI, req.RawQuery, req.Header, req.Body
    if !strings.Contains(req.Body, `"name"`) {
        t.Errorf("expected name in request body, got %s", req.Body)
    }
}

srv.ResetCalls() // clear recorded requests between sub-tests
```

`StringPtr` is a convenience helper for building a `Request` with a body constraint, since `Request.Body` is a `*string`:

```go
trenchcoat.Request{
    Method: "POST",
    URI:    "/api/users",
    Body:   trenchcoat.StringPtr(`{"name": "alice"}`),
}
```

### Terraform provider acceptance tests

Trenchcoat works well as a mock backend in Terraform provider acceptance tests. Point the provider's base URL at `srv.URL` and define coats for each API call the provider makes during the plan/apply cycle.

```go
func TestAccResourceWidget_basic(t *testing.T) {
    srv := trenchcoat.NewServer(
        trenchcoat.WithCoats(
            trenchcoat.Coat{
                Name:    "create-widget",
                Request: trenchcoat.Request{Method: "POST", URI: "/api/v1/widgets"},
                Response: &trenchcoat.Response{
                    Code:    201,
                    Headers: map[string]string{"Content-Type": "application/json"},
                    Body:    `{"id": "widget-1", "name": "test-widget"}`,
                },
            },
            trenchcoat.Coat{
                Name:    "read-widget",
                Request: trenchcoat.Request{Method: "GET", URI: "/api/v1/widgets/widget-1"},
                Response: &trenchcoat.Response{
                    Code:    200,
                    Headers: map[string]string{"Content-Type": "application/json"},
                    Body:    `{"id": "widget-1", "name": "test-widget"}`,
                },
            },
            trenchcoat.Coat{
                Name:    "delete-widget",
                Request: trenchcoat.Request{Method: "DELETE", URI: "/api/v1/widgets/widget-1"},
                Response: &trenchcoat.Response{Code: 204},
            },
        ),
    )
    srv.Start(t)

    resource.Test(t, resource.TestCase{
        ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
        Steps: []resource.TestStep{
            {
                Config: testAccWidgetConfig(srv.URL),
                Check: resource.ComposeAggregateTestCheckFunc(
                    resource.TestCheckResourceAttr("myprovider_widget.test", "name", "test-widget"),
                ),
            },
        },
    })
}

func testAccWidgetConfig(baseURL string) string {
    return fmt.Sprintf(`
provider "myprovider" {
  base_url = %q
}

resource "myprovider_widget" "test" {
  name = "test-widget"
}
`, baseURL)
}
```

For providers that make multiple calls to the same endpoint (e.g. reading a resource during plan and again during apply), response sequences let you return different responses on successive calls:

```go
trenchcoat.Coat{
    Name:    "read-widget-sequence",
    Request: trenchcoat.Request{Method: "GET", URI: "/api/v1/widgets/widget-1"},
    Responses: []trenchcoat.Response{
        {Code: 404, Body: `{"error": "not found"}`},           // pre-create read
        {Code: 200, Body: `{"id": "widget-1", "name": "w1"}`}, // post-create read
        {Code: 200, Body: `{"id": "widget-1", "name": "w1"}`}, // refresh
    },
    Sequence: "once",
}
```

More examples can be found in [`examples/go-tests/example_test.go`](examples/go-tests/example_test.go).

## Building from source

```sh
git clone https://github.com/yesdevnull/trenchcoat.git
cd trenchcoat
make build
```

Available Makefile targets:

| Target | Description |
|---|---|
| `make build` | Build the `trenchcoat` binary. |
| `make test` | Run all tests with race detection. |
| `make coverage` | Run tests and generate `coverage.html`. |
| `make lint` | Run `golangci-lint`. |
| `make clean` | Remove build artifacts and test cache. |
