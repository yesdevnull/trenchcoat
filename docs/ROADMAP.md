# Trenchcoat Roadmap

Open proposals first, ranked by benefit against complexity, then what has
shipped, then what was considered and declined. Declined items keep their
reasoning, so the same idea does not get re-proposed from scratch.

Complexity is scope, not elapsed time. **Low** — a change within one package,
roughly a few hundred lines with its tests. **Medium** — a new field or flag
threaded through parsing, validation, matching and the docs, or a change that
touches two packages. **High** — a new subsystem, a new subcommand, or a change
to the coat file format that existing coats must survive.

---

## Open

### 1. Passthrough Mode

**Benefit:** High | **Complexity:** Medium

A hybrid of serve and proxy: serve matched coats as mocks, forward unmatched
requests to a real upstream. A `--passthrough <upstream-url>` flag on `serve`.

This is the incremental-mocking workflow the tool is named for — start by
proxying everything, then replace real calls with coats one at a time, and the
suite keeps passing throughout. It is the highest-value item open, and the
cheapest of the ambitious ones: both halves already exist and work, so the job
is routing and merging the two request paths rather than new machinery.

Worth deciding up front whether a passthrough request is also *captured*, which
would make it a one-command version of the proxy-then-serve loop.

### 2. Sharing Response Fragments Across Files

**Benefit:** Medium | **Complexity:** Medium

Note the reduced scope: duplication *within* one YAML file is already solved by
anchors and merge keys, and the idiom is documented in the README.

```yaml
coats:
  - name: first
    request: {uri: /a}
    response: &defaults
      code: 200
      headers: {Content-Type: application/json}
  - name: second
    request: {uri: /b}
    response:
      <<: *defaults
```

What that does not cover is the live case:

- Sharing across **files**, which anchors cannot span.
- **JSON** coat files, which have no anchors at all.

A top-level `defaults` block would address both, but it is a coat file format
change: parsing is strict and `coats` is currently the only top-level key a file
may hold, so `defaults` has to be added to the parser, the validator, the JSON
Schema and the docs together. An `imports:` list is the more general answer and
the more expensive one; prefer `defaults` unless a concrete need for imports
turns up.

### 3. Proxy TLS Listener

**Benefit:** Medium | **Complexity:** Low-Medium

`Proxy` has `Start` but no `StartTLS`, so the proxy's own listener is plain HTTP
only. Connections *to* the upstream already do TLS, including
`--tls-server-name` and a TLS 1.2 floor; it is only the client-facing side that
cannot.

This blocks capturing from a client that will not talk to a plain-HTTP proxy, or
one whose base URL must be `https://` for its own reasons. The mock server
already has `StartTLS` and self-signed certificate generation to model it on, so
most of the work is a flag, a listener and tests.

### 4. OpenAPI / Swagger Import

**Benefit:** Medium | **Complexity:** High

Generate coat files from an OpenAPI spec, most likely as a `trenchcoat generate`
subcommand. Useful for bootstrapping mocks for a large API.

The original objection was that mapping a schema to a realistic response body
needs heuristics. That is less true than it looks: OpenAPI carries `example` and
`examples` on both schemas and responses, and a spec that has them needs no
heuristics at all. Scope it to "use the examples the spec provides, skip what it
does not" and the item shrinks a long way. Generating plausible bodies from bare
type information is the part to leave out.

---

## Implemented

### Tier 1 — High Benefit, Low Complexity

- **Request Body Glob/Substring Matching (#1)** — `body_match` field supports
  `exact` (default), `glob`, `contains`, and `regex` modes.
- **Request Assertions / Call Counting (#2)** — `AssertCalled`,
  `AssertCalledN`, `AssertNotCalled`, `Requests`, and `ResetCalls` on the
  programmatic API.
- **Coat-Level Variable Substitution (#3)** — `${VAR}` and `${VAR:-default}`
  syntax resolved from environment variables at parse time.
- **Response Templating (#4)** — Response bodies containing `{{` are rendered
  as Go `text/template` with request context (`.Method`, `.Path`, `.Body`,
  `.Query "key"`, `.Segment N`).
- **Public API TLS Support (#5)** — `WithSelfSignedTLS()` and `WithTLS(cert,
  key)` options with auto-generated certificates and pre-configured
  `TLSClient`.

### Tier 2 — Medium Benefit, Low-Medium Complexity

- **Latency Jitter (#6)** — `delay_jitter_ms` field adds random delay (0 to
  jitter value) on top of `delay_ms`.
- **Proxy-to-Coat Workflow Improvements (#7)** — `--pretty-json` for formatted
  JSON capture, `--body-file-threshold` for large body extraction to separate
  files, `--name-template` for custom captured coat file naming.

### Improvement Ideas

- **Better 404 Diagnostics (A)** — Verbose mode includes ranked near-miss
  diagnostics explaining why each coat didn't match.
- **Coat Validation Warnings (B)** — Non-fatal warnings for duplicate coat
  names and regex patterns expressible as simpler globs.
- **Request Logging Improvements (C)** — Verbose logs include matched coat
  file path, decisive qualifiers (headers/query/body), and structured fields.
- **Glob Pattern Enhancement (D)** — URI glob matching supports `**` for
  multi-segment matching via the `doublestar` library.

---

## Archived / Declined

### ~~Conditional Responses (Request-Aware Sequences)~~

Proposed a `when:` condition on individual responses with `sequence: match`, so
one coat could serve both a "normal" and a "retry" case. **Declined — the
motivating example already works**, using two coats and body matching:

```yaml
coats:
  - name: retry-case
    request:
      method: POST
      uri: /work
      body: '"retry": true'
      body_match: contains
    response: {code: 200, body: retried-ok}
  - name: default-case
    request: {method: POST, uri: /work}
    response: {code: 503, body: unavailable}
```

A body constraint counts towards specificity, so the constrained coat outranks
the unconstrained one and the fallthrough works without any ordering tricks. The
proposal's stated benefit was avoiding "separate coat definitions", which is a
cosmetic saving over something that already works, paid for with a second
matching language inside the response list.

The one thing it would have added that this does not is a *sequence* that
branches — and that is the stateful-behaviour idea below, which was declined on
its own merits.

### ~~Stateful Mock Behaviour~~

Coats defining state transitions — "after `POST /users` succeeds, `GET /users`
returns the created user". **Declined.** It is a state machine expressed in
YAML, which means a language with no debugger, no types and no way to assert on
a transition that did not fire. Sequences cover the ordered case, and anything
genuinely stateful is better written as test-level logic against the
programmatic API, where the host language already has all of that. The original
proposal raised this doubt itself.

### ~~TLS Minimum Version Enforcement~~

Proposed enforcing a TLS 1.2 floor on the mock server's listener, on the
assumption that Go's defaults allow TLS 1.0. **Declined — already true.** Go's
own default server minimum is TLS 1.2; probed directly, the server answers a
TLS 1.0 or 1.1 ClientHello with `protocol version not supported` and negotiates
1.2 and 1.3 normally. There is nothing to implement.

### ~~Complex Directory Structure~~

Recursive directory loading with organisational conventions (e.g.
`mocks/users/list.yaml`) and shared config at directory level. **Declined** —
the flat structure with explicit `--coats` paths is sufficient, and the
shared-fragment need is covered by item 2 above without the complexity.

### ~~Request Body Matching~~ (Implemented)

Implemented as exact string matching via the `request.body` field (`*string` —
`nil` means match any body, a set value means exact match), then extended with
`body_match` modes in item #1.

---

## Not a feature, but tracked

**Windows has no test coverage.** CI runs the suite on `ubuntu-latest` only,
while the Build job cross-compiles for Windows and the README offers it as a
supported platform. Unix-only behaviour lives behind `//go:build unix`, so the
paths that differ on Windows — path separators in `body_file` resolution and in
captured coat filenames, signal handling, file locking during capture — are
never exercised. Adding `windows-latest` to the test matrix is cheap; the likely
cost is discovering it has been broken for a while.
