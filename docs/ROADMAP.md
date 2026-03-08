# Trenchcoat Roadmap

## Feature Proposals — Ranked by Benefit vs Complexity

Features are ranked from best ROI (high benefit, low complexity) to lowest.
Complexity: Low (~1-2 days), Medium (~3-5 days), High (~1-2 weeks).

---

### Tier 2 — Medium Benefit, Low-Medium Complexity

#### 8. Conditional Responses (Request-Aware Sequences)

**Benefit:** Medium | **Complexity:** Medium

Sequences currently cycle through responses regardless of request content.
Allow sequences to branch based on request properties:

```yaml
responses:
  - when:
      body: '{"retry": true}'
    code: 200
    body: "retried-ok"
  - code: 503
    body: "unavailable"
sequence: match  # try to match conditions first, fall through to default
```

This would let a single coat handle both the "normal" and "retry" cases
without needing separate coat definitions.

#### 9. Import / Compose Coat Files

**Benefit:** Medium | **Complexity:** Medium

Allow coat files to reference shared definitions to reduce duplication:

```yaml
imports:
  - ./shared-headers.yaml

coats:
  - name: get-users
    request:
      uri: /api/v1/users
    response:
      headers: !inherit shared-headers
      code: 200
      body: '{"users": []}'
```

Or simpler: a `defaults` block at the top of a coat file that applies to all
coats in that file:

```yaml
defaults:
  response:
    headers:
      Content-Type: "application/json"
      X-Request-Id: "*"

coats:
  - name: get-users
    # inherits Content-Type and X-Request-Id headers
    ...
```

The `defaults` approach is simpler and solves 80% of the duplication problem.

---

### Tier 3 — Nice to Have

#### 10. Stateful Mock Behaviour

**Benefit:** Medium | **Complexity:** High

Allow coats to define state transitions — e.g. "after POST /users succeeds,
GET /users returns the created user." This is powerful but adds significant
complexity. Consider whether this is better handled by test-level logic using
the programmatic API rather than in coat files.

#### 11. OpenAPI / Swagger Import

**Benefit:** Medium | **Complexity:** High

Generate coat files from an OpenAPI spec. Useful for bootstrapping mocks for
large APIs, but the mapping from schema to realistic response bodies requires
heuristics. Could be a standalone `trenchcoat generate` subcommand.

#### 12. Passthrough Mode (Existing Proposal)

**Benefit:** Medium | **Complexity:** Medium

A hybrid of serve and proxy. Serve matched coats as mocks, but forward
unmatched requests to a real upstream URL. Becomes a `--passthrough
<upstream-url>` flag on the `serve` subcommand.

This is useful for incremental mocking — start by proxying everything, then
gradually replace real calls with coats. The proxy and server infrastructure
already exist, so the main work is the routing logic and merging the two code
paths.

---

## Implemented

The following features have been implemented and are available:

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

### ~~Complex Directory Structure~~

Support recursive directory loading with organisational conventions (e.g.
`mocks/users/list.yaml`, `mocks/auth/login.yaml`) and potential shared default
headers/config at directory level. **Declined** — the flat structure with
explicit `--coats` paths is sufficient. The `defaults` block proposal (item 9)
addresses the shared config need without the complexity.

### ~~Request Body Matching~~ (Implemented)

Implemented as exact string matching via the `request.body` field (`*string` —
`nil` means match any body, set value means exact match). Proxy capture
includes `--capture-body` flag (default: `true`). Enhanced with `body_match`
modes (glob, contains, regex) in item 1.
