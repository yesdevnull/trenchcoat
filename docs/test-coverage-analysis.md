# Test Coverage Analysis

Analysis of the Trenchcoat test suite, run with Go 1.25.7 using
`go test -race -coverprofile`, to identify gaps and areas for improvement.

## Overview

**Total coverage: 57.6% of statements** across 47 tests (all passing, race
detector clean).

| Package | Coverage | Tests | Notes |
|---------|----------|-------|-------|
| `cmd/trenchcoat` | **0.0%** | 0 | Completely untested |
| `trenchcoat` (public API) | **0.0%** | 0* | Exercised by `examples/go-tests/` but coverage not attributed |
| `internal/coat` | **55.6%** | 21 | `load.go` entirely uncovered (0%) |
| `internal/matcher` | **80.6%** | 23 | `Match()` only 58.3%, `ResetSequences()` 0% |
| `internal/server` | **88.7%** | 19 | `logRequest` 50%, `resolveBodyFile` 75% |
| `internal/proxy` | **79.0%** | 8 | `New()` 61.1%, `handleRequest` 75% |
| `internal/config` | **88.9%** | 4 | Home dir discovery untested |

\* The `examples/go-tests/` package uses the public API but is in a separate
module, so its coverage is reported as `[no statements]` and does not
contribute to `trenchcoat.go`'s 0.0% figure.

---

## Per-Function Coverage (0% functions)

These functions have **zero** coverage — no test exercises them at all:

| Function | File | Notes |
|----------|------|-------|
| `main` | `cmd/trenchcoat/main.go:18` | CLI entrypoint |
| `newServeCmd` | `cmd/trenchcoat/serve.go:19` | Serve command setup |
| `runServe` | `cmd/trenchcoat/serve.go:39` | Serve command execution |
| `watchCoats` | `cmd/trenchcoat/serve.go:107` | fsnotify hot-reload loop |
| `newLogger` | `cmd/trenchcoat/serve.go:160` | Logger factory (text/json) |
| `newProxyCmd` | `cmd/trenchcoat/proxy.go:14` | Proxy command setup |
| `runProxy` | `cmd/trenchcoat/proxy.go:35` | Proxy command execution |
| `newValidateCmd` | `cmd/trenchcoat/validate.go:10` | Validate command |
| `LoadPaths` | `internal/coat/load.go:20` | Load coats from file/dir paths |
| `loadDir` | `internal/coat/load.go:45` | Directory loader |
| `loadFile` | `internal/coat/load.go:72` | Single file loader + validation |
| `ValidationError.Error` | `internal/coat/validate.go:16` | Error formatting |
| `ResetSequences` | `internal/matcher/matcher.go:188` | Sequence counter reset |
| `WithCoat` | `trenchcoat.go:65` | Public API option |
| `WithCoats` | `trenchcoat.go:72` | Public API option |
| `WithCoatFile` | `trenchcoat.go:81` | Public API option |
| `WithVerbose` | `trenchcoat.go:90` | Public API option |
| `NewServer` | `trenchcoat.go:98` | Public API constructor |
| `Start` | `trenchcoat.go:108` | Public API start |
| `Stop` | `trenchcoat.go:132` | Public API stop |

---

## Per-Function Coverage (partial, < 85%)

| Function | File | Coverage | Uncovered Paths |
|----------|------|----------|-----------------|
| `parseYAML` | `coat/parse.go:27` | 71.4% | `os.ReadFile` error, `yaml.Unmarshal` error |
| `parseJSON` | `coat/parse.go:40` | 71.4% | `os.ReadFile` error, `json.Unmarshal` error |
| `LoadDirectory` | `coat/parse.go:56` | 82.4% | `os.ReadDir` error |
| `UnmarshalYAML` | `coat/query.go:12` | 80.0% | Invalid YAML node type (not scalar/mapping) |
| `Match` | `matcher/matcher.go:114` | 58.3% | Sequence handling: `once` exhaustion path, `cycle` modulo path |
| `matchesURI` | `matcher/matcher.go:247` | 83.3% | Default `return false` branch (unreachable) |
| `betterThan` | `matcher/matcher.go:215` | 88.9% | Glob literal-length tiebreaker with equal lengths |
| `proxy.New` | `proxy/proxy.go:53` | 61.1% | Empty URL, invalid scheme, missing host errors |
| `Start` (proxy) | `proxy/proxy.go:96` | 72.7% | `MkdirAll` error, `Listen` error |
| `Addr` (proxy) | `proxy/proxy.go:120` | 66.7% | Nil listener branch |
| `handleRequest` (proxy) | `proxy/proxy.go:145` | 75.0% | `NewRequestWithContext` error, upstream error, body read error |
| `shouldCapture` | `proxy/proxy.go:220` | 71.4% | `path.Match` error on invalid filter |
| `captureCoat` | `proxy/proxy.go:232` | 80.0% | `yaml.Marshal` error, `WriteFile` error |
| `singleJoiningSlash` | `proxy/proxy.go:348` | 66.7% | Neither-slash and both-slash branches |
| `Start` (server) | `server/server.go:64` | 75.0% | `net.Listen` error |
| `StartTLS` (server) | `server/server.go:81` | 75.0% | `net.Listen` error |
| `Addr` (server) | `server/server.go:98` | 66.7% | Nil listener branch |
| `logRequest` | `server/server.go:231` | 50.0% | Verbose enabled path (never tested with verbose=true) |
| `resolveBodyFile` | `server/server.go:248` | 75.0% | Ambiguous coat source, absolute path, missing FilePath |

---

## 1. `cmd/trenchcoat/` — 0% Coverage (332 lines)

This is the biggest gap. All 8 functions across 4 files are at 0%.

### Recommended Tests

- **`validate` command**: Pass valid and invalid coat file paths, assert exit
  codes and stdout/stderr output. Call `newValidateCmd().RunE` directly.
- **`serve` TLS flag validation**: Provide `--tls-cert` without `--tls-key`,
  assert the "must be provided together" error from `runServe`.
- **`newLogger`**: Assert `"json"` returns `slog.JSONHandler`, `"text"` returns
  `slog.TextHandler`.
- **`proxy` dedupe validation**: Pass `--dedupe random`, assert error message.
- **`watchCoats`**: Use a temp directory, modify a coat file, assert
  `server.Reload` is called. This is harder to test but has high value.

---

## 2. `trenchcoat.go` (Public API) — 0% Attributed Coverage

The example tests exercise this code at runtime but Go's coverage tool only
counts lines within the package under test. All 7 exported functions read 0%.

### Recommended Tests

Add a `trenchcoat_test.go` file in the root package (or move example tests
to use `package trenchcoat_test`):

- **`TestWithCoatFile`** — create a temp YAML coat, use `WithCoatFile`, `Start`,
  make a request, assert response. This covers `WithCoatFile`, `NewServer`,
  `Start`, and `Stop`.
- **`TestWithCoatFile_InvalidPath`** — non-existent path, assert `Start` calls
  `t.Fatalf` (use a `testing.TB` mock or check `loadErrs`).
- **`TestWithVerbose`** — verify the option sets `s.verbose = true`.
- **`TestStop_BeforeStart`** — call `Stop()` on a `NewServer()` without
  `Start()`, assert no panic.

---

## 3. `internal/coat/` — 55.6% Coverage

The entire `load.go` file (3 functions, 89 lines) has **0% coverage**. The
existing tests only exercise `ParseFile`, `LoadDirectory`, `Validate`, and
`QueryField` unmarshalling — but never `LoadPaths`, `loadDir`, or `loadFile`.

### Recommended Tests

**For `load.go` (highest impact — raises package coverage significantly):**
- **`TestLoadPaths_SingleFile`** — pass a single YAML file, assert loaded coats.
- **`TestLoadPaths_Directory`** — pass a directory, assert all coats loaded in
  lexicographic order.
- **`TestLoadPaths_MixedFilesAndDirs`** — pass a file + a directory together.
- **`TestLoadPaths_NonExistentPath`** — assert error returned in errors slice.
- **`TestLoadPaths_ValidationErrors`** — load a file with an invalid coat (no
  URI), assert validation errors are returned alongside valid coats.

**For parse error paths (raises `parseYAML`/`parseJSON` from 71.4% to 100%):**
- **`TestParseYAML_MalformedSyntax`** — write `":\n  - [bad"` to a `.yaml`,
  assert error.
- **`TestParseJSON_MalformedSyntax`** — write `"{bad"` to a `.json`, assert error.
- **`TestParseFile_NonExistent`** — pass `/no/such/file.yaml`, assert error.

**For validation gaps (raises `validateCoat` from 89.3% to ~100%):**
- **`TestValidate_InvalidRegexURI`** — coat with `~/api/[bad`, assert
  validation error containing "invalid regex".

**For query unmarshal error paths:**
- **`TestQueryField_UnmarshalYAML_InvalidType`** — a YAML sequence `[1, 2]` as
  query value, assert error.
- **`TestQueryField_UnmarshalJSON_InvalidType`** — JSON `[1, 2]` or `123` as
  query, assert error.

---

## 4. `internal/matcher/` — 80.6% Coverage

The `Match` function is only at **58.3%** — the sequence handling paths (cycle
modulo, once exhaustion) are not covered by the matcher tests directly. They are
covered at the server integration level, but not at the unit level. Also
`ResetSequences` is at 0%.

### Recommended Tests

- **`TestMatch_Sequence_Cycle`** — create a matcher with a cycle coat, call
  `Match` 3+ times, assert `ResponseIdx` rotates.
- **`TestMatch_Sequence_Once_Exhausted`** — call `Match` beyond the response
  count, assert `Exhausted == true`.
- **`TestMatch_ResetSequences`** — consume some sequence responses, call
  `ResetSequences()`, assert counter is back to 0.
- **`TestNew_InvalidRegexSkipped`** — coat with `~/[bad`, assert `New` doesn't
  panic, and `Match` returns nil for that URI.
- **`TestMatch_EmptyMatcher`** — `New(nil)` then `Match()`, assert returns nil.
- **`TestMatch_ConcurrentSequences`** — spawn N goroutines hitting the same
  sequence coat concurrently, assert no data race (run with `-race`).

---

## 5. `internal/server/` — 88.7% Coverage

Good overall. The main gaps are `logRequest` (50%), `resolveBodyFile` (75%),
and the `Addr` nil-listener path (66.7%).

### Recommended Tests

- **`TestServe_VerboseLogging`** — start server with `Verbose: true`, make a
  request, assert no crash (raises `logRequest` to 100%).
- **`TestServe_BodyFile_AbsolutePath`** — use an absolute path for `body_file`.
- **`TestServe_BodyFile_ProgrammaticCoat`** — `LoadedCoat` with empty
  `FilePath` and relative `body_file`, assert the file is resolved from cwd.
- **`TestServe_EmptyBody`** — coat with status 204 and no body, assert empty
  response.
- **`TestServe_ConcurrentRequests`** — fire many requests in parallel, assert
  all return correct responses. Run with `-race`.
- **`TestServe_Addr_BeforeStart`** — assert `Addr()` returns `""` and `URL()`
  returns `"http://"`.

---

## 6. `internal/proxy/` — 79.0% Coverage

`proxy.New()` validation is at **61.1%** — none of the 4 error branches are
tested. `handleRequest` at 75% is missing all error paths.

### Recommended Tests

**Validation errors (raises `New` to ~100%):**
- **`TestProxy_New_EmptyURL`** — assert error returned.
- **`TestProxy_New_InvalidScheme`** — `"ftp://example.com"`, assert error.
- **`TestProxy_New_MissingHost`** — `"http://"`, assert error.
- **`TestProxy_New_InvalidURL`** — `"://not-a-url"`, assert error.

**Request handling (raises `handleRequest` from 75%):**
- **`TestProxy_ForwardsRequestBody`** — POST with body, verify upstream receives
  it exactly.
- **`TestProxy_CapturesQueryString`** — request with `?foo=bar`, verify coat
  file contains `query: foo=bar`.
- **`TestProxy_UpstreamUnreachable`** — configure unreachable upstream, assert
  502 response to client.

**Other gaps:**
- **`TestProxy_Dedupe_Overwrite_Replaces`** — same request twice with
  `overwrite`, assert only one file and content is from second request.
- **`TestSingleJoiningSlash`** — test all 4 branches: both have slash, neither,
  only first, only second.
- **`TestProxy_VerboseLogging`** — start proxy with `Verbose: true`, make a
  request, assert no crash.

---

## 7. `internal/config/` — 88.9% Coverage

Only missing the `.trenchcoat.yml` extension path and home directory discovery.

### Recommended Tests

- **`TestLoad_CwdConfig_YML`** — create `.trenchcoat.yml` in temp dir.
- **`TestLoad_ExplicitPath_Missing`** — pass non-existent file, assert error.
- **`TestLoad_MalformedConfig`** — write invalid YAML to config, assert error.

---

## Priority Summary

| Priority | Area | Current | Target | Effort |
|----------|------|---------|--------|--------|
| **High** | `cmd/trenchcoat/` CLI tests | 0.0% | ~60% | Medium |
| **High** | `internal/coat/load.go` (LoadPaths) | 0.0% | ~90% | Low |
| **High** | `trenchcoat.go` public API | 0.0%* | ~80% | Low |
| **High** | `proxy.New()` validation errors | 61.1% | ~100% | Low |
| **Medium** | `matcher.Match()` sequence paths | 58.3% | ~90% | Low |
| **Medium** | Coat parse error paths | 71.4% | ~100% | Low |
| **Medium** | Coat validation: invalid regex URI | 89.3% | ~100% | Low |
| **Medium** | `proxy.handleRequest` error paths | 75.0% | ~90% | Medium |
| **Medium** | Concurrency tests (matcher + server) | n/a | pass `-race` | Medium |
| **Low** | `server.logRequest` verbose path | 50.0% | 100% | Low |
| **Low** | `server.resolveBodyFile` edge cases | 75.0% | ~100% | Low |
| **Low** | Config `.yml` + home dir discovery | 88.9% | ~100% | Low |
| **Low** | Proxy `singleJoiningSlash` | 66.7% | 100% | Low |

\* Coverage is 0% due to how Go's tooling attributes coverage across packages.
The code is exercised by `examples/go-tests/` at runtime.

---

## Estimated Impact

Addressing the **High** and **Medium** priority items would raise overall
coverage from **57.6%** to approximately **75-80%**, with the largest gains
coming from:

1. Testing `internal/coat/load.go` (0% → ~90% adds ~4 percentage points to
   package and ~2 points overall)
2. Adding `cmd/trenchcoat/` tests (0% → ~60% adds ~6 points overall)
3. Adding `trenchcoat.go` tests (0% → ~80% adds ~1 point overall)
4. Testing `proxy.New()` validation and `matcher.Match()` sequences (adds
   ~2 points overall)
