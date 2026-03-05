# Test Coverage Analysis

Analysis of the Trenchcoat test suite, run with Go 1.25.7 using
`go test -race -coverprofile=coverage.out ./...`.

## Current Coverage

**102 tests, all passing, race detector clean. Overall: 91.4% of statements.**

| Package | Coverage | Tests | Change |
|---------|----------|-------|--------|
| `trenchcoat` (public API) | **92.6%** | 9 | +3 |
| `cmd/trenchcoat` | **80.3%** | 24 | +4 |
| `internal/coat` | **96.4%** | 36 | — |
| `internal/config` | **88.9%** | 6 | +2 |
| `internal/matcher` | **96.6%** | 36 | +5 |
| `internal/proxy` | **90.1%** | 21 | +4 |
| `internal/server` | **97.0%** | 26 | +3 |

All packages are above 80% coverage. Overall coverage improved from 89.1% → 91.4%.

---

## Per-Function Coverage

Functions below 100% coverage, ordered by impact:

| Function | Coverage | Notes |
|----------|----------|-------|
| `cmd/trenchcoat/main.go:main` | 0% | CLI entrypoint — cannot unit test |
| `serve.go:watchCoats` | 48.4% | Most branches in event loop untested |
| `proxy.go:singleJoiningSlash` | 66.7% | Missing branch coverage |
| `proxy.go:shouldCapture` | 71.4% | Invalid filter pattern error path |
| `proxy.go:Start` | 72.7% | Listen/mkdir error paths |
| `server.go:startListener` | 75.0% | Listen error path |
| `trenchcoat.go:Start` | 80.0% | Start error path |
| `coat/query.go:UnmarshalYAML` | 80.0% | Missing invalid type branch |
| `matcher.go:matchesURI` | 83.3% | Default case in switch |
| `server.go:resolveBodyFile` | 83.3% | Ambiguity detection branch |
| `proxy.go:handleRequest` | 85.0% | Some error paths |
| `proxy.go:captureCoat` | 86.5% | Gzip decompression errors |
| `proxy.go:New` | 87.0% | Some validation branches |
| `config.go:Load` | 88.9% | Home dir discovery |
| `matcher.go:betterThan` | 88.9% | Tie-breaking branch |
| `serve.go:runServe` | 89.5% | Some error paths |

---

## Tests Inventory

### `trenchcoat_test.go` (root package — 92.6%)

- `TestWithCoat` — single coat, make request, assert response
- `TestWithCoats` — multiple coats, verify both endpoints
- `TestWithCoatFile` — load coat from YAML file
- `TestWithVerbose` — verify verbose option propagates
- `TestStop_BeforeStart` — call Stop() without Start(), no panic
- `TestNewServer_NoOptions` — empty server constructor
- `TestNewServer_NoCoats_Returns404` — no coats → 404 with JSON error body
- `TestWithCoatFile_NonExistent` — non-existent file → load errors
- `TestWithCoatFile_InvalidCoat` — coat without URI → validation errors

### `cmd/trenchcoat/commands_test.go` (80.3%)

- Validate command: valid file, invalid file, non-existent file, directory, no args
- `newLogger`: text, json, unknown (default)
- Serve command: TLS cert without key, TLS key without cert, no coats, with coats, with watch
- Proxy command: invalid dedupe, no args, invalid upstream URL, start-and-stop, verbose mode
- `watchCoats`: non-existent paths, with directory and file
- `TestWatchCoats_FileModificationTriggersReload` — modify coat file triggers reload
- `TestWatchCoats_NonCoatFileIgnored` — non-coat file changes ignored
- `TestWatchCoats_CreateNewCoatFile` — new coat file picked up
- `TestWatchCoats_RemoveCoatFile` — removed coat file triggers reload

### `internal/coat/` (96.4%)

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

### `internal/matcher/` (96.6%)

- Exact/glob/regex URI matching, method+ANY, header globs, query matching, precedence
- Sequences: cycle, once, default cycle, reset, concurrent access (100 goroutines)
- Invalid regex skipped, empty matcher, singular response
- `TestMatch_Precedence_GlobSameLiteralLen_FileOrder` — glob tie-breaking by file order
- `TestMatch_Precedence_GlobMethodANY_vs_Specific` — ANY vs specific method with globs
- `TestMatch_QueryMap_GlobValues` — query value glob patterns
- `TestMatch_QueryMap_MultipleValues` — repeated query params
- `TestMatch_QueryMap_SpecialChars` — URL-encoded query values

### `internal/proxy/` (90.1%)

- Request forwarding, response relay, POST body forwarding, header stripping
- Query string capture, coat file capture, overwrite dedup
- Upstream unreachable (502), verbose logging, gzip decompression
- Validation: empty URL, invalid scheme, missing host, addr before start
- `TestProxy_InvalidGzipBody` — invalid gzip fallback to raw body
- `TestProxy_Filter_InvalidPattern` — malformed glob filter handled gracefully
- `TestSingleJoiningSlash` — full branch coverage for path joining
- `TestProxy_RedirectHandling` — 3xx responses captured and relayed as-is

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

## Proposed Improvements

### Priority 1: High Impact — Untested Core Logic

#### 1. Proxy dedup modes: `skip` and `append` (proxy.go:344-358)

Only `overwrite` dedup is tested. The `skip` and `append` branches in
`generateFilename` are completely untested. These are user-facing features
with distinct behavior:

- **`skip`**: Uses `filepath.Glob` to check for existing files; returns `""`
  to signal skipping. Should verify that: (a) first capture writes a file,
  (b) second capture of the same route is skipped, (c) different routes are
  still captured.
- **`append`**: Uses an internal counter map to generate unique filenames.
  Should verify that: (a) first capture uses a base filename, (b) subsequent
  captures append an incrementing counter, (c) different routes maintain
  independent counters.

#### 2. `watchCoats` event loop (serve.go:104-156, currently 48.4%)

The file watcher's event loop is the lowest-covered non-main function. Key
untested branches:

- **Write/Create events on a coat file** — verify that `srv.Reload` is called
  with newly loaded coats when a `.yaml` file is modified.
- **Remove events** — verify reload triggers when a coat file is deleted.
- **Non-coat file changes** — verify that changes to non-`.yaml`/`.json` files
  are ignored (the `IsCoatFile` guard).
- **Watcher error channel** — verify that errors from `watcher.Errors` are
  logged and don't crash the watcher.
- **Load errors during reload** — verify that validation errors during reload
  are logged as warnings and the server continues operating.

#### 3. `resolveBodyFile` ambiguity detection (server.go:247-270, 83.3%)

The function attempts to detect which coat file a `body_file` is relative to
when multiple coat files define coats with the same name/URI/method. The
ambiguity branch is not directly tested. Should verify:

- Two coat files with the same coat name/URI/method but different `body_file`
  values — the function should fall back and not resolve the file.
- Single coat with `body_file` across multiple files — no ambiguity, resolves
  correctly.

### Priority 2: Medium Impact — Error Paths, Edge Cases, and TLS

#### 4. TLS configuration and error handling

TLS is currently tested only on the happy path (valid self-signed cert →
successful HTTPS request). Several important scenarios are missing:

**a. Invalid or mismatched cert/key files (server.go:77-80)**

`StartTLS` delegates to `http.Server.ServeTLS` which validates the cert/key
pair. The server goroutine logs the error but no test verifies the behavior:

- **Mismatched cert and key** — the cert was generated with a different key
  than the one provided. `ServeTLS` returns an error immediately. Verify
  the server fails to start or logs the mismatch error.
- **Corrupt PEM file** — a key file containing garbage data. Verify the error
  is surfaced.
- **Wrong PEM type** — e.g. a certificate file provided as the key argument
  and vice versa.

**b. Expired certificate handling**

Generate a cert with `NotAfter` in the past. The server will start (Go only
validates on handshake), but clients should receive a TLS handshake error.
Test that:

- The server starts without error.
- An HTTPS request from a client that checks expiry (default behavior)
  returns a `tls.CertificateExpiredError` or equivalent.
- The server itself remains running (doesn't crash).

**c. TLS via the public API (`trenchcoat.go`)**

The public `Server` type has no TLS support — there is no `StartTLS` method
or `WithTLS` option. This means Go test suites using the programmatic API
cannot test HTTPS endpoints. Proposed additions:

- `WithTLS(certFile, keyFile string) Option` — configure TLS cert/key.
- Modify `Start(t)` to call `inner.StartTLS` when TLS is configured, and
  set `s.URL` to `https://...`.
- Alternatively, a simpler `WithSelfSignedTLS() Option` that generates an
  ephemeral self-signed cert at startup (useful for tests that just need
  HTTPS without caring about specific certs).
- Tests:
  - `TestWithTLS` — start server with TLS, make HTTPS request, verify body.
  - `TestWithSelfSignedTLS` — same, using auto-generated cert.
  - `TestStartTLS_InvalidCert` — verify `t.Fatal` is called on bad cert.

**d. TLS minimum version enforcement**

The server currently uses Go's default TLS config, which allows TLS 1.0+.
For security hardening, the server should enforce a minimum TLS version
(e.g. TLS 1.2). Test that:

- A TLS 1.2 client connects successfully.
- A TLS 1.1 client (if configured with `tls.Config{MaxVersion: tls.VersionTLS11}`)
  is rejected with a handshake error.

**e. Proxy mode TLS listener**

The proxy server (`internal/proxy`) only supports plain HTTP for its
listener. If the proxy itself should serve over HTTPS (e.g. for HTTPS proxy
testing), this is a feature gap rather than a test gap. At minimum, document
this limitation or add a `StartTLS` method to `Proxy`.

**f. CLI `--tls-cert`/`--tls-key` with non-existent files**

The CLI validates that both flags are provided together, but doesn't
validate that the files exist before passing them to `StartTLS`. A test
should verify that:

- `trenchcoat serve --tls-cert /nonexistent --tls-key /nonexistent` returns
  a clear error (currently fails inside `ServeTLS`).
- The error message includes the file path for debuggability.

#### 5. Gzip decompression errors in proxy capture (proxy.go:258-271)

~~Two error branches in `captureCoat` are untested.~~ **Now covered** by
`TestProxy_InvalidGzipBody`, which verifies the fallback to raw body when
`gzip.NewReader` fails. The `io.ReadAll` mid-stream failure remains untested
but is low risk.

#### 5b. `shouldCapture` with invalid filter pattern (proxy.go:242-251)

~~The `path.Match` error path is not tested.~~ **Now covered** by
`TestProxy_Filter_InvalidPattern`, which verifies that a malformed glob
causes `shouldCapture` to return false and no coat file is written.

#### 6. `singleJoiningSlash` branch coverage (proxy.go:395-404)

~~Only two branches are exercised.~~ **Now covered** by `TestSingleJoiningSlash`
with table-driven cases for the reachable branches: both slashes (upstream
trailing + request leading) and default (no trailing upstream slash). The
`!aslash && !bslash` branch is unreachable via HTTP since request paths
always have a leading `/`.

#### 7. Query matching edge cases (matcher.go:275-307)

~~No tests confirming these behaviors.~~ **Now covered** by:

- `TestMatch_QueryMap_MultipleValues` — multiple values for the same query
  param (e.g. `?tag=a&tag=b`), verifying any-value matching.
- `TestMatch_QueryMap_SpecialChars` — URL-encoded query values.
- `TestMatch_QueryMap_GlobValues` — glob patterns in query value matching.

#### 8. Matcher precedence tie-breaking (matcher.go:216-235)

~~The file-order tie-breaking path is not exercised.~~ **Now covered** by
`TestMatch_Precedence_GlobSameLiteralLen_FileOrder`, which uses two glob
patterns that both match the same request with equal literal prefix length,
verifying the earlier-defined coat wins.

### Priority 3: Lower Impact — Hardening and Completeness

#### 9. Config file parsing errors (config.go)

~~No test covers malformed YAML config files.~~ **Now covered** by
`TestLoad_InvalidYAML` (malformed YAML returns error) and
`TestLoad_CwdConfig_YmlExtension` (`.trenchcoat.yml` discovery in CWD).

#### 10. Public API with invalid coat files

~~The public API's error behavior is untested.~~ **Now covered** by
`TestWithCoatFile_NonExistent` (non-existent file produces load errors)
and `TestWithCoatFile_InvalidCoat` (coat without URI produces validation
errors).

#### 11. Server with no coats handling requests

~~No test for empty server 404 behavior.~~ **Now covered** by
`TestNewServer_NoCoats_Returns404`, which verifies the JSON error body.

#### 12. Proxy redirect handling

~~No tests for 3xx capture/relay.~~ **Now covered** by
`TestProxy_RedirectHandling`, which verifies a 301 is captured as-is
and relayed to the client without following the redirect.

#### 13. Concurrent requests during server reload

~~No test for concurrent reload correctness.~~ **Now covered** by
`TestServe_Reload_ConcurrentRequests`, which fires 100 concurrent requests
while toggling 20 reloads and verifies each response is coherent. Runs
with `-race` to detect data races.

---

## Summary

| Priority | Area | Estimated Tests | Impact |
|----------|------|-----------------|--------|
| P1 | Proxy `skip`/`append` dedup | 4-6 | Core feature, zero coverage |
| P1 | `watchCoats` event loop | 4-5 | 48.4% coverage, core hot-reload |
| P1 | `resolveBodyFile` ambiguity | 2-3 | Correctness risk |
| P2 | TLS invalid/mismatched cert+key | 3-4 | Error resilience, security |
| P2 | TLS expired certificate | 2 | Security correctness |
| P2 | TLS in public API (`WithTLS`) | 3-4 | Feature gap, API completeness |
| P2 | TLS minimum version enforcement | 2 | Security hardening |
| P2 | CLI `--tls-*` with missing files | 1-2 | Error UX |
| P2 | Gzip decompression errors | 2 | Error resilience |
| P2 | `shouldCapture` invalid filter | 1 | Error path |
| P2 | `singleJoiningSlash` branches | 1 (table) | Low-risk utility |
| P2 | Query matching edge cases | 3-4 | Subtle matching bugs |
| P2 | Matcher `betterThan` tie-break | 1-2 | Precedence correctness |
| P3 | Config parsing errors | 1-2 | Hardening |
| P3 | Public API error handling | 2 | API robustness |
| P3 | Server no-coats requests | 1 | Completeness |
| P3 | Proxy redirect capture | 1-2 | Documented behavior |
| P3 | Concurrent reload correctness | 1-2 | Race safety |
| P3 | Proxy TLS listener | 1-2 | Feature gap documentation |
