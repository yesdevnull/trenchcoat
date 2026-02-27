# Test Coverage Analysis

Analysis of the Trenchcoat test suite to identify gaps and areas for improvement.

## Overview

The codebase has 23 Go files (11 source, 8 test, 4 CLI command) totalling ~4,500
lines. Tests exist for all internal packages (`coat`, `matcher`, `server`,
`proxy`, `config`) plus example integration tests for the public API. The
`cmd/trenchcoat/` CLI layer has zero test coverage.

---

## 1. `cmd/trenchcoat/` — No Tests (332 lines, 0% coverage)

This is the biggest gap. All four CLI files are completely untested.

| File | Lines | What's untested |
|------|-------|-----------------|
| `main.go` | 40 | Root command setup, version flag, config `PersistentPreRunE` |
| `serve.go` | 169 | TLS flag validation, `watchCoats()` fsnotify loop, `newLogger()` |
| `proxy.go` | 94 | Dedupe flag validation, full proxy CLI flow |
| `validate.go` | 29 | Validate command with valid/invalid coat files |

### Recommended Tests

- Test `validate` command with valid and invalid coat files (assert exit codes and output).
- Test `serve` TLS flag validation (error when only one of `--tls-cert`/`--tls-key` provided).
- Test `newLogger()` returns correct handler type for "json" vs "text".
- Test dedupe flag validation in proxy command (invalid value should error).
- These can be done by calling the cobra command's `RunE` directly.

---

## 2. `trenchcoat.go` (Public API) — Partially Tested

The `examples/go-tests/` exercises `WithCoat`, `WithCoats`, `Start`, and `Stop`,
but several options and error paths are untested.

| Gap | Lines | Description |
|-----|-------|-------------|
| `WithCoatFile()` | 81-87 | Loading coats from a file path — never tested directly |
| `WithVerbose()` | 90-94 | Verbose option — never tested |
| `Start()` load errors | 111-113 | `t.Fatalf` called on `loadErrs` — not tested |
| `Stop()` nil inner | 132-135 | Calling `Stop()` before `Start()` — not tested |

### Recommended Tests

- `TestWithCoatFile` — create a temp YAML file, use `WithCoatFile`, make a request.
- `TestWithCoatFile_InvalidPath` — non-existent path, assert `Start()` calls `t.Fatalf`.
- `TestWithVerbose` — ensure the option propagates to the server config.
- `TestStop_BeforeStart` — should not panic.

---

## 3. `internal/coat/` — Good Coverage, Missing Error Paths

The parsing and validation happy paths are well-covered. Key gaps:

| Gap | File:Line | Description |
|-----|-----------|-------------|
| Malformed YAML | `parse.go:34` | Invalid YAML syntax — error path never tested |
| Malformed JSON | `parse.go:47` | Invalid JSON syntax — error path never tested |
| Non-existent file | `parse.go:29,42` | `os.ReadFile` error — never tested |
| Invalid regex URI | `validate.go:54-58` | Coat with invalid regex like `~/api/[bad` — not tested |
| `ValidationError.Error()` unnamed | `validate.go:17-21` | Falls back to `coat[N]` format — not asserted |
| `LoadPaths` non-existent path | `load.go:26-28` | `os.Stat` error branch — not tested |
| `LoadPaths` mixed files/dirs | `load.go:31-39` | Both files and directories — not tested |
| `UnmarshalYAML` unknown node | `query.go:29` | YAML sequence as query — error branch not tested |
| `UnmarshalJSON` invalid type | `query.go:49` | JSON number/array as query — error branch not tested |

### Recommended Tests

- `TestParseYAML_MalformedSyntax` — invalid YAML, assert error.
- `TestParseJSON_MalformedSyntax` — invalid JSON, assert error.
- `TestParseFile_NonExistent` — file that doesn't exist, assert error.
- `TestValidate_InvalidRegexURI` — coat with `~/api/[bad`, assert validation error.
- `TestLoadPaths_NonExistentPath` — assert error in returned errors slice.
- `TestLoadPaths_MixedFilesAndDirs` — pass a file + dir, assert all coats loaded.
- `TestQueryField_UnmarshalYAML_InvalidType` — sequence as query, assert error.
- `TestQueryField_UnmarshalJSON_InvalidType` — array as query, assert error.

---

## 4. `internal/matcher/` — Good Coverage, Missing Concurrency and Edge Cases

| Gap | Line | Description |
|-----|------|-------------|
| Invalid regex skipped | `matcher.go:68-71` | `New()` with invalid regex — never tested |
| `ResetSequences()` | `matcher.go:188-194` | Only tested indirectly via `server.Reload` |
| Empty matcher | — | `Match()` with zero coats — never tested |
| Concurrent sequence access | `matcher.go:159-178` | Thread safety under `-race` — never tested |
| Multiple regex file order | — | Two regex coats matching same path — not tested |
| Query map glob `?` | — | Query glob with `?` character — not tested |

### Recommended Tests

- `TestNew_InvalidRegexSkipped` — coat with bad regex, assert no panic and it's skipped.
- `TestMatch_EmptyMatcher` — `New(nil)`, assert `Match()` returns nil.
- `TestMatch_ResetSequences` — directly test counter reset.
- `TestMatch_ConcurrentSequences` — spawn goroutines hitting same sequence coat.
- `TestMatch_MultipleRegex_FileOrder` — two regex coats, assert first-defined wins.

---

## 5. `internal/server/` — Good Coverage, Missing Edge Cases

| Gap | Line | Description |
|-----|------|-------------|
| `resolveBodyFile` ambiguous | `server.go:258` | Two coats with same identity from different files — not tested |
| `resolveBodyFile` absolute path | `server.go:264-268` | Absolute `body_file` path — not tested |
| `resolveBodyFile` no FilePath | `server.go:250-261` | Programmatic coat (empty `FilePath`) — not tested |
| `Addr()` nil listener | `server.go:98-103` | Returns "" before `Start()` — not tested |
| Verbose logging | `server.go:231-244` | `logRequest` with verbose — not tested |
| Concurrent requests | — | Multiple simultaneous requests — not tested under `-race` |
| Empty body response | `server.go:203-205` | 204 No Content with empty body — not tested |

### Recommended Tests

- `TestServe_BodyFile_AbsolutePath` — absolute `body_file` path.
- `TestServe_BodyFile_ProgrammaticCoat` — empty `FilePath` with relative `body_file`.
- `TestServe_EmptyBody` — status 204, no body.
- `TestServe_ConcurrentRequests` — parallel requests with `-race`.
- `TestServe_Addr_BeforeStart` — assert returns `""`.

---

## 6. `internal/proxy/` — Moderate Coverage, Missing Validation and Error Paths

| Gap | Line | Description |
|-----|------|-------------|
| `New()` empty URL | `proxy.go:64-65` | Empty upstream — not tested |
| `New()` invalid scheme | `proxy.go:72-74` | E.g. `ftp://` — not tested |
| `New()` missing host | `proxy.go:75-77` | E.g. `http://` — not tested |
| Request body forwarding | `proxy.go:153,163` | POST body through proxy — not tested |
| Query in captured coat | `proxy.go:269-271` | Query params captured — not tested |
| Upstream unreachable | `proxy.go:178-181` | 502 error path — not tested |
| `singleJoiningSlash()` | `proxy.go:348-358` | All four branches — not tested |
| Dedupe overwrite | — | Same request twice, assert overwrite — not tested |
| Verbose logging | `proxy.go:204-212` | With `Verbose: true` — not tested |

### Recommended Tests

- `TestProxy_New_EmptyURL` — assert error returned.
- `TestProxy_New_InvalidScheme` — `ftp://`, assert error.
- `TestProxy_New_MissingHost` — assert error.
- `TestProxy_ForwardsRequestBody` — POST with body, verify upstream receives it.
- `TestProxy_CapturesQueryString` — verify coat file contains query.
- `TestProxy_UpstreamUnreachable` — invalid upstream, assert 502.
- `TestProxy_Dedupe_Overwrite_Replaces` — same request twice, assert one file.
- `TestSingleJoiningSlash` — test all four branches.

---

## 7. `internal/config/` — Partially Tested

| Gap | Line | Description |
|-----|------|-------------|
| `.trenchcoat.yml` extension | `config.go:26` | Only `.yaml` tested, not `.yml` |
| Home dir config | `config.go:36-43` | `~/.config/trenchcoat/config.yaml` — not tested |
| Missing explicit path | `config.go:20` | `--config` to missing file — not tested |
| Malformed config | — | Invalid YAML in config — not tested |

### Recommended Tests

- `TestLoad_CwdConfig_YML` — use `.trenchcoat.yml` extension.
- `TestLoad_ExplicitPath_Missing` — point to non-existent file, assert error.
- `TestLoad_MalformedConfig` — invalid YAML content, assert error.

---

## Priority Summary

| Priority | Area | Impact | Effort |
|----------|------|--------|--------|
| **High** | `cmd/trenchcoat/` CLI tests | 332 lines with 0% coverage | Medium |
| **High** | `trenchcoat.go` `WithCoatFile` + error paths | Key public API feature untested | Low |
| **High** | Proxy validation (`New()` error paths) | 4 error branches with 0% coverage | Low |
| **Medium** | Coat parsing error paths (malformed YAML/JSON, missing files) | Robust error handling | Low |
| **Medium** | Coat validation: invalid regex URI | Important validation rule untested | Low |
| **Medium** | Concurrency tests (matcher sequences, server requests) | Thread safety under `-race` | Medium |
| **Medium** | `resolveBodyFile` edge cases | 3 untested branches | Low |
| **Low** | Config `.yml` extension and home dir discovery | Edge cases | Low |
| **Low** | Proxy `singleJoiningSlash()` | Small utility | Low |
| **Low** | Query unmarshal error paths | Defensive error handling | Low |
