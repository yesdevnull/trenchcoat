# Trenchcoat

Extensible mock, and proxy-to-mock, HTTP server written in Go.

## Project Overview

Trenchcoat is a CLI tool with two modes:

1. **Serve mode** — mock HTTP server matching requests against "coat" definitions
2. **Proxy mode** — HTTP proxy capturing request/response pairs as coat files

Module path: `github.com/yesdevnull/trenchcoat`

**`README.md` is the specification.** The coat file format, URI and value
matching, precedence, the programmatic API and proxy capture behaviour are all
documented there, and it is the version a user reads. Read it before changing
matching, parsing or capture behaviour, and update it in the same commit as the
change. This file covers what a user does not need to know: internal structure,
the invariants that are not obvious from the code, and how to work on the repo.

That split exists because it was previously violated. The coat spec lived in
both files, they diverged, and CLAUDE.md was right about header globs while the
README told users the opposite. Two copies of a spec is two chances to be wrong.

## Repository Structure

```
cmd/trenchcoat/           CLI entrypoint (cobra commands: serve, proxy, validate)
  main.go                 Root command, signal handling, version info
  help.txt                Long help, go:embed-ed into the binary — a doc surface
  serve.go                Serve subcommand with hot-reload file watching
  proxy.go                Proxy subcommand
  validate.go             Validate subcommand
  commands_test.go        CLI integration tests
internal/
  coat/                   Types, parsing, validation for coat files
    types.go              Core types: File, Coat, Request, Response, QueryField
    parse.go              YAML/JSON parsing, ${VAR} substitution, strict decoding
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
scripts/
  coverage-report.sh      On-demand coverage report (backs `make coverage`)
  regenerate-demo.sh      Rebuilds docs/demo.md against the working tree
docs/
  demo.md                 CLI demo walkthrough (Showboat-generated)
  demo-fixtures/          Coat files the demo serves — it cannot run without them
  ROADMAP.md              Future feature plans
trenchcoat.go             Public API package for Go test integration
trenchcoat_test.go        Public API tests
README.md                 User-facing specification — see above
coatfile.schema.json      JSON Schema for coat files (hand-maintained)
.golangci.yml             Linter set, so `make lint` and CI agree
.claude/
  settings.json           Checked-in Claude Code config (PostToolUse hooks)
  hooks/
    gofmt-on-write.py     Formats each Go file as it is edited
    coat-schema-sync.py   Warns when coat types drift from coatfile.schema.json
    test_hooks.py         Tests for both hooks
.github/workflows/
  ci.yaml                 Test, lint, vet, format, hooks, goreleaser check, build
  release.yaml            Tag-triggered GoReleaser publish
.goreleaser.yaml          GoReleaser config for cross-platform releases
renovate.json             Renovate dependency auto-update config
```

## Documentation surfaces

Four files describe the coat format or the CLI, and nothing enforces that they
agree. Every one of them has drifted at least once. When you change behaviour,
work this table:

| Change | Update |
|---|---|
| Any coat field added, removed or renamed | `README.md`, `cmd/trenchcoat/help.txt`, `coatfile.schema.json` |
| Any validation rule | `README.md`, `coatfile.schema.json` |
| Any CLI flag added or changed | `README.md`, `cmd/trenchcoat/help.txt` |
| Matching, precedence or capture behaviour | `README.md`, and `help.txt` if it contradicts |
| Internal invariant or repo workflow | this file |

`help.txt` is `go:embed`-ed, so a stale line there ships in the binary — that is
how `--tls-server-name` went five months undocumented in `trenchcoat --help`.

`coatfile.schema.json` hand-mirrors the coat schema and nothing in the build or
tests enforces that they agree; the `coat-schema-sync.py` hook warns when you
edit `internal/coat/types.go` or `validate.go` without touching the schema.

`docs/demo.md` is Showboat-generated from real command output. Do not hand-edit
it — every `output` block is only worth something because a command produced it.
Run `scripts/regenerate-demo.sh` instead, or `--check` to see what has drifted
without changing the file. The script builds the binary from the working tree,
copies `docs/demo-fixtures/` into a throwaway directory and runs there, and
forces `TZ=UTC`; getting any of those wrong records misleading output.

Showboat is run as `uvx showboat@latest`, so `uv` is the only prerequisite —
there is nothing to install first. CI runs the same `--check` in the Demo Drift
job, so forgetting to regenerate fails the build rather than going unnoticed.

The demo's shell blocks wait for each listener to accept a connection instead of
sleeping a fixed second. Keep it that way: a `sleep` long enough to be reliable
in CI is dead time on every run, and one short enough to feel fast is a flake.
Wait on the port with bash's `/dev/tcp` rather than curl — a curl probe against
the *proxy* forwards a request upstream and captures it, which changes the very
output the document is recording.

Changing a command *shown* in the demo, rather than its output, is the one case
the script does not cover: use `uvx showboat@latest extract docs/demo.md` to
emit the command list, edit that, and rebuild from it.

## Development

### Requirements

- Go 1.25.x. The exact patch release lives in the `toolchain` directive in
  `go.mod`; CI installs it via `go-version-file: go.mod`, and Renovate bumps it.
  The `go` directive stays at `1.25` as the minimum for consumers of the package.
- golangci-lint. The version is pinned in `.github/workflows/ci.yaml` and the
  linter set in `.golangci.yml`, so a local run matches CI.

### Installing Go

If Go 1.25+ is not installed or the auto-download via `GOTOOLCHAIN` fails (e.g.
due to DNS/network restrictions), install manually. Renovate keeps the version
below current — do not replace it with a placeholder, the `customManagers` entry
in `renovate.json` matches on it.

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
make coverage       # Coverage report + coverage.html (scripts/coverage-report.sh)
make lint           # Run golangci-lint
make clean          # Clean build artifacts and test cache

go test ./...                           # Run all tests
go test -v -race -count=1 ./...        # Verbose with race detector
go vet ./...                            # Static analysis
gofmt -w .                              # Format code
goimports -w .                          # Fix imports
golangci-lint run ./...                 # Lint
govulncheck ./...                       # Vulnerability check

scripts/coverage-report.sh --min 90     # Functions under 90% covered
scripts/coverage-report.sh --help       # Full options
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

Both scripts are plain stdlib Python with no dependencies, and must stay
runnable under **Python 3.9** — macOS ships 3.9.6 as `/usr/bin/python3`, and the
`#!/usr/bin/env python3` shebang resolves to whatever is first on `PATH`. That
rules out PEP 604 (`str | None`) annotations unless
`from __future__ import annotations` is present. The CI Hooks job runs the tests
on 3.9 for exactly this reason.

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

Every feature begins with a test. Do not write implementation code without a
corresponding failing test first.

## Architecture Notes

The user-facing behaviour these implement is specified in `README.md`. What
follows is what the README does not say.

### Size limits

| Constant | Value | Governs |
|---|---|---|
| `matcher.maxBodyMatchSize` | 1 MiB | Request body read for `request.body` matching. A larger body never matches a body-constrained coat, but is still restored in full for downstream handlers. |
| `server.maxRecordBodySize` | 1 MiB | Request body recorded for assertions and exposed to templates as `.Body`. Truncated with a marker. |
| `proxy.maxBodySize` | 10 MiB | Request and response bodies through the proxy. Over it, the request is 413 and the response 502. |
| `coat.MaxDelayMs` | 60000 | `delay_ms` + `delay_jitter_ms` combined, rejected at validation. |

Request bodies are read lazily and reconstituted via `httputil.ReconstitutedBody`
so a body consumed for matching is still readable by the handler. Anything that
reads a request body must go through it.

### Invalid coats from the programmatic API

Validation and `WithCoatFile` reject a coat defining neither `response` nor
`responses`, so a file can never produce one. `WithCoat`/`WithCoats` take a
`Coat` as given, so they can. Such a request returns **500** naming the coat and
logs at `ERROR` — it is a fault in the coat, not a request that found no match,
and must not be conflated with the 404 path.

The same reasoning applies to `sequence`: validation restricts it to `cycle` and
`once`, but the programmatic API accepts anything, so `resolveSequence` treats
every unrecognised value as `cycle` rather than indexing off the end of
`Responses`.

### Matcher internals

- Precedence is a `matchScore` sorted by `betterThan`; the ordering is
  documented for users in `README.md` and must not drift from it.
- An invalid regex URI or `body_match: regex` pattern keeps its entry, with the
  compiled regex left nil, so the coat still appears in near-miss diagnostics
  but can never match.
- `literalLen` (glob tie-breaking) stops at every glob metacharacter, `[`
  included — a character class is not literal prefix, and counting it as such
  lets a vaguer pattern outrank a more specific one.
- `globCache` hangs off the `Matcher`, not the package, so a hot reload discards
  the previous generation's compiled patterns instead of accumulating every
  pattern the process has ever seen.

### Key Dependencies

| Package         | Purpose                        |
|-----------------|--------------------------------|
| cobra           | CLI framework                  |
| viper           | Config file and flag binding   |
| fsnotify        | Hot-reload file watching       |
| doublestar/v4   | Glob matching with `**` support|
| slog (stdlib)   | Structured logging             |
| gopkg.in/yaml.v3 | YAML parsing                 |

## Testing Expectations

- Unit tests for matcher: exact, glob, regex URI; method+ANY; header globs; query matching; precedence
- Unit tests for coat parsing/validation (YAML, JSON, mutual exclusivity rules, strict decoding, `${VAR}` substitution)
- Integration tests for serve mode (start server, send requests, assert responses)
- Integration tests for proxy mode (proxy through, assert captured coat files)
- Tests for response sequences (cycle and once modes)
- Tests for hot-reload (modify coat file on disk, verify server picks up changes)
- Tests for TLS (self-signed cert)
- Tests for the public API (`trenchcoat_test.go`)
- Tests for CLI commands (`commands_test.go`)

Unix-only behaviour (symlinks, signals, FIFOs) lives in `*_unix_test.go` behind
a `//go:build unix` tag. CI runs the suite on `ubuntu-latest` only, so those
tests always run there and Windows-specific behaviour is not covered by tests.

Run `scripts/coverage-report.sh` for current coverage. There is deliberately no
coverage report committed to the repository — the one that used to be here
drifted five months before anyone noticed.

## CI

`.github/workflows/ci.yaml`:

- **Test** — `go test -v -count=1 -race -coverprofile=coverage.out` (uploads coverage artifact)
- **Lint** — golangci-lint, version pinned in the workflow, linters in `.golangci.yml`
- **Vet & Static Analysis** — `go vet`, `go mod tidy` check, `govulncheck` (fails the job)
- **Format** — `gofmt -l`, `goimports -l`
- **Claude Code Hooks** — `.claude/hooks/test_hooks.py` on Python 3.9
- **Demo Drift** — `scripts/regenerate-demo.sh --check`, so a behaviour change cannot leave `docs/demo.md` advertising the old behaviour
- **GoReleaser Config** — `goreleaser check`
- **Build** — cross-compile linux/darwin/windows × amd64/arm64, needs all of the above

Tool versions are pinned in the workflow `env` block, each behind a
`# renovate:` annotation that a customManager in `renovate.json` reads. Do not
reintroduce `@latest` installs — they make a run unreproducible and let an
upstream release break an unrelated pull request.

`paths-ignore` skips the entire workflow for a docs-only change. That is safe
only while no job here is a required status check; if branch protection is
enabled on `main`, drop the filters.

`.github/workflows/release.yaml` runs GoReleaser on a `v*` tag.

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
- British spelling in prose and comments — `misspell` enforces it in Go files
