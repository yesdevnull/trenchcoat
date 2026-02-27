# Test Coverage Analysis

Analysis of the Trenchcoat test suite, run with Go 1.25.7 using
`go test -race -coverprofile`.

## Current Coverage (after improvements)

**82 tests, all passing, race detector clean.**

| Package | Before | After | Tests |
|---------|--------|-------|-------|
| `trenchcoat` (public API) | 0.0% | **92.6%** | 6 |
| `cmd/trenchcoat` | 0.0% | **79.7%** | 20 |
| `internal/coat` | 55.6% | **94.8%** | 36 |
| `internal/config` | 88.9% | **88.9%** | 4 |
| `internal/matcher` | 80.6% | **96.8%** | 31 |
| `internal/proxy` | 79.0% | **87.4%** | 17 |
| `internal/server` | 88.7% | **92.5%** | 23 |

All packages are now above 70% coverage.

---

## Tests Added

### `trenchcoat_test.go` (root package — 0% → 92.6%)

- `TestWithCoat` — single coat, make request, assert response
- `TestWithCoats` — multiple coats, verify both endpoints
- `TestWithCoatFile` — load coat from YAML file
- `TestWithVerbose` — verify verbose option propagates
- `TestStop_BeforeStart` — call Stop() without Start(), no panic
- `TestNewServer_NoOptions` — empty server constructor

### `cmd/trenchcoat/commands_test.go` (0% → 79.7%)

- Validate command: valid file, invalid file, non-existent file, directory, no args
- `newLogger`: text, json, unknown (default)
- Serve command: TLS cert without key, TLS key without cert, no coats, with coats, with watch
- Proxy command: invalid dedupe, no args, invalid upstream URL, start-and-stop, verbose mode
- `watchCoats`: non-existent paths, with directory and file

### `internal/coat/load_test.go` (load.go: 0% → covered)

- `TestLoadPaths_SingleFile` — single YAML file
- `TestLoadPaths_Directory` — directory of YAML files
- `TestLoadPaths_MixedFilesAndDirs` — file + directory together
- `TestLoadPaths_NonExistentPath` — error for missing paths
- `TestLoadPaths_ValidationErrors` — validation errors alongside valid coats
- `TestLoadPaths_Empty` — empty input
- `TestLoadPaths_SubdirSkipped` — subdirectories not recursed

### `internal/coat/validate_test.go` (error paths)

- `TestValidationError_Error_WithName` / `_WithoutName` — error formatting
- `TestValidate_InvalidRegexURI` — invalid regex URI validation
- `TestParseYAML_MalformedSyntax` / `TestParseJSON_MalformedSyntax` — parse error paths
- `TestParseFile_NonExistent` / `_NonExistentJSON` — missing file errors
- `TestQueryField_UnmarshalJSON_InvalidType` — invalid JSON type for query

### `internal/matcher/sequence_test.go` (Match: 58.3% → covered)

- `TestMatch_Sequence_Cycle` — cycle through responses
- `TestMatch_Sequence_Once_Exhausted` — once sequence exhaustion
- `TestMatch_Sequence_DefaultCycle` — default sequence is cycle
- `TestMatch_ResetSequences` — reset counter to 0
- `TestNew_InvalidRegexSkipped` — bad regex coat skipped, good coats still work
- `TestMatch_EmptyMatcher` — nil coats returns nil match
- `TestMatch_ConcurrentSequences` — 100 goroutines, no data race
- `TestMatch_SingularResponse` — singular Response field (not Responses)

### `internal/proxy/proxy_validation_test.go` (New: 61.1% → covered)

- `TestProxy_New_EmptyURL` / `_InvalidScheme` / `_MissingHost` — validation errors
- `TestProxy_Addr_BeforeStart` — empty address before start
- `TestProxy_ForwardsRequestBody` — POST body forwarding
- `TestProxy_CapturesQueryString` — query string in captured coat
- `TestProxy_UpstreamUnreachable` — 502 for unreachable upstream
- `TestProxy_Dedupe_Overwrite` — overwrite deduplication
- `TestProxy_Verbose` — verbose logging mode

### `internal/server/verbose_test.go` (logRequest: 50% → covered)

- `TestServe_VerboseLogging` — verbose logging for matched and unmatched requests
- `TestServe_EmptyBody` — 204 with no body
- `TestServe_Addr_BeforeStart` — empty address before start
- `TestServe_BodyFile_AbsolutePath` — absolute path for body_file

---

## Remaining Gaps

These areas still have room for improvement but are all above the 70% threshold:

| Area | Current | Notes |
|------|---------|-------|
| `cmd/trenchcoat/main.go` (`main`) | 0% | CLI entrypoint — hard to unit test |
| `internal/config` home dir discovery | 88.9% | Edge case, low impact |
| `proxy.Start` listen/mkdir errors | ~75% | Hard to trigger in tests |
| `server.Start`/`StartTLS` listen errors | ~75% | Hard to trigger in tests |
| `proxy.shouldCapture` invalid filter | ~71% | Edge case |
| `proxy.singleJoiningSlash` all branches | ~67% | Low impact utility |
