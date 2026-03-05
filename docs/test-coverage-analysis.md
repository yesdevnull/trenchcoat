# Test Coverage Analysis

Analysis of the Trenchcoat test suite, run with Go 1.25.7 using
`go test -race -coverprofile=coverage.out ./...`.

## Current Coverage

**180 tests, all passing, race detector clean. Overall: 90.5% of statements.**

| Package | Coverage | Tests |
|---------|----------|-------|
| `trenchcoat` (public API) | **92.9%** | 10 |
| `cmd/trenchcoat` | **78.2%** | 24 |
| `internal/coat` | **95.5%** | 36 |
| `internal/config` | **88.9%** | 6 |
| `internal/matcher` | **94.6%** | 36 |
| `internal/proxy` | **90.3%** | 29 |
| `internal/server` | **97.0%** | 26 |

All packages are above 78% coverage.

---

## Per-Function Coverage

Functions below 100% coverage, ordered by impact:

| Function | Coverage | Notes |
|----------|----------|-------|
| `cmd/trenchcoat/main.go:main` | 0% | CLI entrypoint — cannot unit test |
| `proxy.go:Start` | 72.7% | Listen/mkdir error paths |
| `trenchcoat.go:Start` | 80.0% | Start error path |
| `coat/query.go:UnmarshalYAML` | 80.0% | Missing invalid type branch |
| `matcher.go:matchesURI` | 83.3% | Default case in switch (unreachable) |
| `matcher.go:matchesBody` | 83.3% | Body read error path |
| `singleJoiningSlash` | 83.3% | `!aslash && !bslash` unreachable via HTTP |
| `proxy.go:handleRequest` | 85.0% | Some error paths |
| `proxy.go:runProxy` | 86.7% | Some flag/validation paths |
| `server.go:startListener` | 87.5% | Listen error path |
| `config.go:Load` | 88.9% | Home dir discovery |
| `matcher.go:betterThan` | 88.9% | Some tie-breaking branches |
| `serve.go:runServe` | 89.5% | Some error paths |
| `proxy.go:captureCoat` | 90.0% | `io.ReadAll` mid-stream gzip failure |

---

## Tests Inventory

### `trenchcoat_test.go` (root package — 92.9%)

- `TestWithCoat` — single coat, make request, assert response
- `TestWithCoats` — multiple coats, verify both endpoints
- `TestWithCoatFile` — load coat from YAML file
- `TestWithVerbose` — verify verbose option propagates
- `TestStop_BeforeStart` — call Stop() without Start(), no panic
- `TestNewServer_NoOptions` — empty server constructor
- `TestWithCoat_BodyMatching` — body-based request matching (alice vs bob)
- `TestNewServer_NoCoats_Returns404` — no coats → 404 with JSON error body
- `TestWithCoatFile_NonExistent` — non-existent file → load errors
- `TestWithCoatFile_InvalidCoat` — coat without URI → validation errors

### `cmd/trenchcoat/commands_test.go` (78.2%)

- Validate command: valid file, invalid file, non-existent file, directory, no args
- `newLogger`: text, json, unknown (default)
- Serve command: TLS cert without key, TLS key without cert, no coats, with coats, with watch
- Proxy command: invalid dedupe, no args, invalid upstream URL, start-and-stop, verbose mode, no-headers, no-headers with strip-headers conflict
- `watchCoats`: non-existent paths, with directory and file
- `TestWatchCoats_FileModificationTriggersReload` — modify coat file triggers reload
- `TestWatchCoats_NonCoatFileIgnored` — non-coat file changes ignored
- `TestWatchCoats_CreateNewCoatFile` — new coat file picked up
- `TestWatchCoats_RemoveCoatFile` — removed coat file triggers reload

### `internal/coat/` (95.5%)

- YAML/JSON parsing, query string/map, responses plural, default method
- File extension handling (.yaml, .yml, .json, unknown)
- Validation: missing URI, both response/responses, neither, body/body_file, sequence rules
- QueryField unmarshalling (YAML string/map, JSON string/map, error cases)
- LoadPaths: single file, directory, mixed, non-existent, validation errors, empty, subdir skip
- ValidationError formatting with/without name, invalid regex URI, malformed syntax

### `internal/config/` (88.9%)

- Explicit config path, no config file, CWD config, nested config structure
- `TestLoad_InvalidYAML` — malformed YAML returns error
- `TestLoad_CwdConfig_YmlExtension` — `.trenchcoat.yml` discovered in CWD

### `internal/matcher/` (94.6%)

- Exact/glob/regex URI matching, method+ANY, header globs, query matching, precedence
- Sequences: cycle, once, default cycle, reset, concurrent access (100 goroutines)
- Invalid regex skipped, empty matcher, singular response
- `TestMatch_Precedence_GlobSameLiteralLen_FileOrder` — glob tie-breaking by file order
- `TestMatch_Precedence_GlobMethodANY_vs_Specific` — ANY vs specific method with globs
- `TestMatch_QueryMap_GlobValues` — query value glob patterns
- `TestMatch_QueryMap_MultipleValues` — repeated query params
- `TestMatch_QueryMap_SpecialChars` — URL-encoded query values

### `internal/proxy/` (90.3%)

- Request forwarding, response relay, POST body forwarding, header stripping
- Query string capture, coat file capture, overwrite/skip/append dedup
- Upstream unreachable (502), verbose logging, gzip decompression
- Validation: empty URL, invalid scheme, missing host, addr before start
- `TestProxy_CaptureBody_Default` — POST body captured by default
- `TestProxy_CaptureBody_Disabled` — body omitted when capture disabled
- `TestProxy_InvalidGzipBody` — invalid gzip fallback to raw body
- `TestProxy_Filter_InvalidPattern` — malformed glob filter handled gracefully
- `TestSingleJoiningSlash` — reachable branch coverage for path joining
- `TestProxy_RedirectHandling` — 3xx responses captured and relayed as-is
- `TestProxy_NoHeaders` — all headers omitted from captured coats when NoHeaders=true
- `TestProxy_NoHeaders_StripHeaders_MutuallyExclusive` — NoHeaders and StripHeaders conflict rejected

### `internal/server/` (97.0%)

- Response bodies, headers, 404s, status codes, glob/regex, delays
- BodyFile resolution (relative + absolute), missing body_file (500)
- Verbose logging, empty body (204), addr before start
- Hot reload, sequence reset on reload, TLS connectivity
- `TestServe_BodyFile_AmbiguousCoatSources` — ambiguous body_file detection
- `TestServe_BodyFile_SameCoatFilePath_NoAmbiguity` — same source, no ambiguity
- `TestServe_TLS_CorruptKeyFile` — garbage PEM key data
- `TestServe_TLS_MismatchedCertAndKey` — cert/key from different keypairs
- `TestServe_TLS_ExpiredCert` — expired cert rejected, server stays running
- `TestServe_Reload_ConcurrentRequests` — concurrent requests during reload (race-safe)

---

## Remaining Gaps

### TLS via the public API (`trenchcoat.go`)

The public `Server` type has no TLS support — there is no `StartTLS` method
or `WithTLS` option. This means Go test suites using the programmatic API
cannot test HTTPS endpoints. Proposed additions:

- `WithTLS(certFile, keyFile string) Option` — configure TLS cert/key.
- Modify `Start(t)` to call `inner.StartTLS` when TLS is configured, and
  set `s.URL` to `https://...`.
- Alternatively, a simpler `WithSelfSignedTLS() Option` that generates an
  ephemeral self-signed cert at startup (useful for tests that just need
  HTTPS without caring about specific certs).

### TLS minimum version enforcement

The server currently uses Go's default TLS config, which allows TLS 1.0+.
For security hardening, the server should enforce a minimum TLS version
(e.g. TLS 1.2). Test that a TLS 1.1 client is rejected with a handshake
error while a TLS 1.2+ client connects successfully.

### Proxy mode TLS listener

The proxy server (`internal/proxy`) only supports plain HTTP for its
listener. If the proxy itself should serve over HTTPS (e.g. for HTTPS proxy
testing), this is a feature gap rather than a test gap. At minimum, document
this limitation or add a `StartTLS` method to `Proxy`.

### CLI `--tls-cert`/`--tls-key` with non-existent files

The CLI validates that both flags are provided together, but doesn't
validate that the files exist before passing them to `StartTLS`. A test
should verify that the error message includes the file path for
debuggability.
