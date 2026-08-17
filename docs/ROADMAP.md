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

### 1. Call Reporting for Non-Go Consumers

**Benefit:** High | **Complexity:** Low

`serve --report <file>`, writing a JSON summary at shutdown: how many times each
coat matched, and every request that matched nothing. Plus `--strict`, which
exits non-zero if anything went unmatched.

This closes the tool's widest asymmetry. `Server.CallCount` already records the
data (`internal/server/server.go:441`), but it is reachable only through the Go
API — `serve` has no flag for it. So a Python, Node or shell test suite can
point at a trenchcoat mock and get responses, but can never assert "the code
under test called `POST /widgets` exactly twice", which is half the reason to
use a mock rather than a static fixture server.

`--strict` answers the other question a test suite wants: did my coats actually
cover everything the code called, or did something quietly 404? Both are
presentation on top of data the server already keeps, which is what makes this
the best return on the list.

### 2. Response Body Redaction on Capture

**Benefit:** High | **Complexity:** Medium

`--strip-headers` defaults to redacting `Authorization`, `Cookie` and
`Set-Cookie`, so the threat model is already understood — but only for headers.
Response *bodies* are captured verbatim, so capturing any OAuth or login flow
writes `access_token`, `refresh_token` and `id_token` into a coat file that
someone then commits.

A `--redact-body` taking either JSON field paths or regexes would close it. Field
paths are the better default: they are precise, they survive pretty-printing, and
they fail safe on a body that is not JSON. Sensible defaults are worth
considering too, matching the spirit of the `--strip-headers` defaults, though
guessing at field names is less reliable than knowing header names.

This is the only item on the list that is a security gap rather than a missing
convenience, which is why it outranks cheaper work below it.

### 3. Passthrough Mode

**Benefit:** High | **Complexity:** Medium

A hybrid of serve and proxy: serve matched coats as mocks, forward unmatched
requests to a real upstream. A `--passthrough <upstream-url>` flag on `serve`.

This is the incremental-mocking workflow the tool is named for — start by
proxying everything, then replace real calls with coats one at a time, and the
suite keeps passing throughout. It is the cheapest of the ambitious items: both
halves already exist and work, so the job is routing and merging the two request
paths rather than new machinery.

Worth deciding up front whether a passthrough request is also *captured*, which
would make it a one-command version of the proxy-then-serve loop.

### 4. `trenchcoat explain`

**Benefit:** Medium | **Complexity:** Low

`trenchcoat explain --coats <paths> <METHOD> <URI>`, printing the ranked
candidate coats and the criterion that decided between them.

Precedence has five tie-breakers — URI mode, qualifier count, glob literal
prefix, specific method over `ANY`, then definition order. Answering "why did
*that* coat win?" currently means starting a server, sending a real request and
reading verbose logs. `MatchVerbose` already produces ranked near-miss
diagnostics; this is largely a matter of exposing them without a live server, and
of reporting the winner's reasoning as well as the losers'.

Pairs naturally with item 5 — the same machinery answers both "why did this
win?" and "can this ever win?".

### 5. Unreachable-Coat Warnings

**Benefit:** Medium | **Complexity:** Low-Medium

Dead-code detection for coat files, reported by `validate` as a warning.

A coat with the same URI, method and qualifiers as an earlier one can never be
reached: the earlier one wins on definition order every time. Today that is
invisible until someone wonders why editing a mock changes nothing. The matcher
already computes everything needed in `matchScore` and `betterThan`, and
`ValidateWithWarnings` already has a warning channel with two occupants
(duplicate names, over-complex regex), so this extends existing machinery rather
than adding any.

Worth keeping conservative: warn only where a coat is *provably* shadowed —
identical or strictly-broader predecessor — rather than attempting to prove
unreachability across glob and regex patterns in general, which is not decidable
cheaply.

### 6. Capture Filters Beyond the URI

**Benefit:** Low-Medium | **Complexity:** Low

`shouldCapture` takes only the URL path (`internal/proxy/proxy.go:434`), so
`--filter` can express "only `/api/*`" and nothing else. Two obvious asks are
currently impossible: "capture only successful responses" and "capture
everything except the auth endpoints".

`--filter-status` (a code or range list) and `--filter-method` would cover both,
and a negation form for `--filter` would cover the second more directly. Small,
self-contained, and it composes with item 2 — the cleanest way to avoid
capturing a token is often to not capture that endpoint at all.

### 7. Proxy TLS Listener

**Benefit:** Medium | **Complexity:** Low-Medium

`Proxy` has `Start` but no `StartTLS`, so the proxy's own listener is plain HTTP
only. Connections *to* the upstream already do TLS, including `--tls-server-name`
and a TLS 1.2 floor; it is only the client-facing side that cannot.

This blocks capturing from a client that will not talk to a plain-HTTP proxy, or
one whose base URL must be `https://` for its own reasons. The mock server
already has `StartTLS` and self-signed certificate generation to model it on, so
most of the work is a flag, a listener and tests.

### 8. Capture Replay Verification

**Benefit:** Medium | **Complexity:** Medium

After capturing, start a server from the captures and re-issue the recorded
requests, reporting any that do not replay to the response that was recorded.

This is aimed squarely at the documented footguns: `Accept` is dropped from
captures, encoded paths collapse to their decoded form, and glob metacharacters
in a path are escaped. Each produces a capture that looks right and silently
fails to replay — and the content-negotiation case actively serves the *wrong*
body. Turning "your coats are subtly wrong" from a debugging session into a
command is worth more here than in most tools, because these failures are
inherent to what capture has to discard rather than bugs to be fixed.

Overlaps with item 1: verification needs to know which coats were hit, which is
the same bookkeeping `--report` exposes.

### 9. Sharing Response Fragments Across Files

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

### 10. Response Throttling and Slow Bodies

**Benefit:** Medium | **Complexity:** Medium

`delay_ms` and `delay_jitter_ms` are applied entirely *before* the response
(`internal/server/server.go:254`), and the body is then written in a single call.
So a coat can model a slow *server* but not a slow *transfer*, and a client's
read timeout, progress reporting or partial-response handling cannot be tested at
all.

A `throttle_bytes_per_sec` on the response, writing the body in chunks with the
appropriate pauses, would cover it. Depends on the same flushing work as item 11,
so the two are worth doing together or not at all.

Note the interaction with `WriteTimeout`, currently set to
`MaxDelayMs + 30s`: a throttled body makes the total response time a function of
body size, so the timeout needs rethinking rather than a larger constant.

### 11. Streaming and Server-Sent Events

**Benefit:** Medium | **Complexity:** High

Nothing in the server uses `http.Flusher`; a response body is written in one
call after `WriteHeader`. That means an entire category of client cannot be
mocked at all — Server-Sent Events, chunked streaming APIs, and the streaming
responses that most LLM and log-tailing APIs now use.

The coat format would need a way to express a sequence of chunks with delays
between them, which is a genuine format change rather than a new field:

```yaml
response:
  code: 200
  headers: {Content-Type: text/event-stream}
  stream:
    - {data: "event: start\n\n", delay_ms: 0}
    - {data: "event: token\n\n", delay_ms: 50}
```

Note this is a *third* meaning of "a sequence" alongside `responses` and the
proposed conditional sequences — worth naming carefully so the format does not
end up with three similar concepts that behave differently.

### 12. OpenAPI / Swagger Import

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

The `#N` below are the original proposal IDs these shipped under, kept so old
discussion still resolves. They are unrelated to the numbering of the open items
above, which is just rank order and shifts as items land or are declined.

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
shared-fragment need is covered by item 9 above without the complexity.

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
