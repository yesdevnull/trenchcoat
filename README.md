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

A request that matches no coat returns **404** with a JSON body naming the
method and URI. Run with `--verbose` to have the log explain, for each coat, how
close it came and which qualifier ruled it out.

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

#### Which request headers are captured

Every request header a coat records becomes a **match constraint when that coat
is replayed**. A capture that recorded `User-Agent: curl/8.5.0` would 404 for
every client that is not that exact version of curl, so client- and
connection-specific headers are deliberately dropped:

| Dropped | Kept |
|---|---|
| Hop-by-hop headers, and any header a peer scopes to one hop by naming it in `Connection` | `Content-Type` |
| `Host`, `Content-Length` | Custom headers, e.g. `X-Api-Key` |
| `User-Agent`, `Accept`, `Accept-Encoding`, `Accept-Language` | Everything else not listed as dropped |

This has a known cost against a **content-negotiating upstream**, where `Accept`
selects the representation rather than describing the client. Capturing
`GET /doc` with `Accept: application/json` and again with
`Accept: application/xml` produces the same file name, so under the default
`--dedupe overwrite` the second capture replaces the first — and the surviving
coat holds the XML body with no `Accept` constraint, so replaying the JSON
request returns XML. Capture such an upstream one representation at a time into
separate `--write-dir`s, and add the `Accept` header to the coats by hand.

#### Other capture behaviour

- Requests larger than 10 MiB are rejected with 413 and never reach the upstream; upstream responses larger than 10 MiB fail the request with 502. Neither is captured.
- A captured path containing a glob metacharacter is recorded with those characters escaped, so the coat means the path it was captured from: `/api/items[abc]` is recorded as `/api/items\[abc\]`. Left unescaped it would be read as a character class, and an unbalanced bracket would not compile at all.
- The captured `request.uri` is the **decoded** path, so `/files/dir%2Ffile` and `/files/dir/file` collapse to one coat even though they are different requests upstream. Forwarding preserves the original encoding; only the capture collapses.
- `Content-Length` is never recorded on a captured response either — the captured body differs from the upstream body whenever it is pretty-printed or decompressed, and `net/http` derives the correct length when serving.
- Gzipped upstream responses are decompressed before being written to the coat file, so captures stay readable. The client still receives the response as the upstream sent it.
- Redirects are not followed. The 3xx response is relayed and captured as-is.
- `http_proxy`, `https_proxy`, and `no_proxy` are respected for upstream connections.
- To proxy to an upstream whose certificate is issued for a different hostname than the address it is served from, pass `--tls-server-name` with the hostname the certificate covers. It becomes both the SNI name sent upstream and the name verified against; chain and expiry verification stay enabled.
- To proxy to an upstream whose certificate uses a negative serial number (rejected since Go 1.23), start the proxy with `GODEBUG=x509negativeserial=1`. See the [Go documentation](https://go.dev/doc/godebug#x509negativeserial).

### `trenchcoat validate`

Validate coat files for schema correctness without starting a server.

```
trenchcoat validate <path>...
```

Exits 0 if all files are valid, non-zero with diagnostics if any errors are found. Warnings are printed to stderr but do not cause a non-zero exit; they cover duplicate coat names, and regex URIs simple enough to be written as globs.

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
| Glob | Contains `*`, `?` or `[` | `/api/v1/users/*` | `/api/v1/users/123`, `/api/v1/users/abc` |
| Glob | `**` spans path segments | `/api/**/posts/*` | `/api/v1/users/1/posts/9` |
| Regex | Prefixed with `~/` | `~/api/v1/users/\d+` | `/api/v1/users/123` but not `/api/v1/users/abc` |

`[` makes a URI a glob because the underlying matcher reads it as a character
class, so `/items[abc]` matches `/itemsa` and not `/items[abc]`. Escape it —
`/items\[abc\]` — to mean the literal path; the URI is still matched as a glob
either way, so there is no way to make a bracketed URI an exact match.

Regex URIs are anchored as a group, `^(?:pattern)$`, so a top-level alternation
is bounded as a whole: `~/users|/accounts` matches `/users` and `/accounts`, and
neither `/users/extra` nor `/x/y/accounts`.

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

### Two glob dialects

Header values, query values and `body_match: glob` patterns use a *different*
glob dialect from URIs, because none of those values are paths:

| Metacharacter | In a URI glob | In a header, query or body glob |
|---|---|---|
| `*` | Any run of characters, stopping at `/` | Any run of characters, `/` and newlines included |
| `**` | Spans path segments | No special meaning |
| `?` | Any single character except `/` | Any single character |
| `[` | Opens a character class | Literal `[` |

So `Content-Type: "*"` matches `application/json`, and a `body_match: glob`
pattern of `*` matches a multi-line body — where the same `*` in a URI would
stop at the first `/`.

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

An exhausted `once` sequence returns 404, the same as a request that matched no
coat at all.

### Response body templating

A response body containing `{{` is rendered as a Go
[`text/template`](https://pkg.go.dev/text/template) with the incoming request as
its context. This happens after `body_file` is resolved, so the contents of a
`body_file` are templated too.

| Field | Meaning |
|---|---|
| `.Method` | Request method |
| `.Path` | Request URL path |
| `.Body` | Request body, capped at 1 MiB |
| `.Query "page"` | First value of a query parameter, `""` if absent |
| `.Segment 2` | Nth path segment, 0-indexed from the root, `""` if absent |

```yaml
coats:
  - name: echo-user
    request:
      uri: "~/api/users/\\d+"
    response:
      code: 200
      body: '{"id": {{.Segment 2}}, "method": "{{.Method}}"}'
```

A body that legitimately contains `{{` is treated as a template whether you
meant it to be or not. A template that fails to parse is returned unrendered and
silently; one that fails to execute is returned unrendered with a warning
logged.

### Variable substitution

`${VAR}` and `${VAR:-default}` in a coat file are replaced from the environment
before the file is parsed. The substitution is textual and runs over the whole
file, so it works in any field — URIs, headers, bodies, `body_file` paths.

```yaml
coats:
  - name: env-driven
    request:
      uri: "${API_PREFIX:-/api/v1}/users"
      headers:
        Authorization: "Bearer ${API_TOKEN}"
    response:
      code: 200
      body: '{"env": "${DEPLOY_ENV:-local}"}'
```

`:-` follows shell semantics: the default applies when the variable is unset
**or** set to an empty string. Without `:-`, a variable that is set but empty
substitutes as empty. A variable that is unset and has no default is left
in the file verbatim, as the literal text `${VAR}` — it is not an error, so a
typo in a variable name shows up as a mock that fails to match rather than as a
load failure.

Because substitution runs before parsing and templating, `${...}` is resolved at
load time from the server's environment, while `{{...}}` is resolved per request
from the request. The two do not interfere.

### Validation rules

`trenchcoat validate` — and loading a coat file in either mode — enforces:

- `request.uri` is required.
- Exactly one of `response` (singular) or `responses` (plural). Neither is an error, both is an error.
- `body` and `body_file` are mutually exclusive, in both the singular and plural forms.
- `sequence` is only valid alongside `responses`, and must be `cycle` or `once`.
- A URI containing `*`, `?` or `[` must be a valid glob pattern.
- A regex URI (`~/` prefix) must compile as a Go regular expression.
- `body_match` must be `exact`, `glob`, `contains` or `regex`, and requires `request.body` to be set. With `regex`, `request.body` must compile as a Go regular expression.
- `body_file` must be a relative path with no `..` components.
- `delay_ms` and `delay_jitter_ms` must be non-negative, neither may exceed 60000, and nor may their sum.

Containment of `body_file` is enforced again when the response is served, after
symlinks are resolved: a `body_file` that points outside the coat file's own
directory returns 500 rather than serving the target. Validation cannot catch
this, because a symlink can be repointed after the file validates.

Non-fatal **warnings** are reported alongside errors for duplicate coat names,
and for regex URIs simple enough to be expressed as globs. Warnings go to stderr
and do not affect the exit code.

### Strict parsing

Coat files are parsed strictly in both YAML and JSON: an unknown key is an error
naming the field, not a key that is silently ignored. `coats` is the only
top-level key a coat file may hold, and a file must contain exactly one
document — a second YAML `---` document is rejected rather than ignored.

To share a response fragment between coats without repeating it, anchor on the
response of the first coat that uses it and merge from the ones after. There is
no top-level holder key to anchor on, because there is no top-level key but
`coats`:

```yaml
coats:
  - name: first
    request: {uri: /a}
    response: &defaults        # anchor on a real field
      code: 200
      headers: {Content-Type: application/json}
  - name: second
    request: {uri: /b}
    response:
      <<: *defaults            # code 200 and Content-Type merged in
  - name: third
    request: {uri: /c}
    response:
      <<: *defaults
      code: 201                # merged values can be overridden
```

### Editor support

[`coatfile.schema.json`](coatfile.schema.json) is a JSON Schema for the coat
file format. Point your editor at it for completion and inline validation — with
the YAML Language Server (VS Code, Neovim and others), add a modeline to the top
of a coat file:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/yesdevnull/trenchcoat/main/coatfile.schema.json
coats:
  - name: hello
    ...
```

The schema is maintained by hand alongside the validator, so treat `trenchcoat
validate` as the authority where the two disagree.

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
| `make lint` | Run `golangci-lint` with the config in `.golangci.yml`. |
| `make clean` | Remove build artifacts and test cache. |

`scripts/coverage-report.sh` backs `make coverage` and takes options of its own —
`--functions` or `--min N` to list what is under-covered, `--html` for the
browsable report. Run it with `--help` for the full set.

## Releases

Tagged releases are built by GoReleaser and published as GitHub releases with
tar.gz (zip on Windows) archives and a checksums file, for people who want a
binary without a Go toolchain. `go install` resolves tags through the module
proxy and does not depend on them.
