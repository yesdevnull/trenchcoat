# Trenchcoat Roadmap

## Feature Proposals — Ranked by Benefit vs Complexity

Features are ranked from best ROI (high benefit, low complexity) to lowest.
Complexity: Low (~1-2 days), Medium (~3-5 days), High (~1-2 weeks).

---

### Tier 1 — High Benefit, Low Complexity

#### 1. Request Body Glob/Substring Matching

**Benefit:** High | **Complexity:** Low

Body matching is already implemented as exact string match. Extending it to
support glob patterns (like headers already do) and substring/contains matching
would make it dramatically more useful for real-world API testing where request
bodies contain timestamps, UUIDs, or other dynamic fields.

Proposed syntax in coat files:

```yaml
request:
  body_match: glob       # or: exact (default), contains, regex
  body: '{"name": "*", "email": "*@example.com"}'
```

The matcher infrastructure already handles glob via `path.Match` for headers
and URIs — this reuses existing patterns. The `body_match` field would default
to `exact` for backwards compatibility.

#### 2. Request Assertions / Call Counting (Programmatic API)

**Benefit:** High | **Complexity:** Low

The test API (`trenchcoat.NewServer`) currently has no way to verify that
expected requests were actually made. This is the single biggest gap for test
usage. Add:

```go
srv.AssertCalled(t, "get-users")           // coat was called at least once
srv.AssertCalledN(t, "get-users", 3)       // called exactly N times
srv.AssertNotCalled(t, "delete-users")     // never called
srv.Requests("get-users") []http.Request   // return captured requests
```

Implementation: add a `sync.Map` of coat-name → `[]capturedRequest` in the
server, populated in `handleRequest`. Minimal changes, high test utility.

#### 3. Response Templating (Basic Variable Substitution)

**Benefit:** High | **Complexity:** Low-Medium

Allow response bodies to reference parts of the matched request using simple
template variables. This avoids needing separate coats for every variation:

```yaml
response:
  body: '{"id": "{{.Request.URI.Segment 3}}", "method": "{{.Request.Method}}"}'
```

Or simpler built-in variables:

```yaml
response:
  body: '{"echo": "{{.Request.Body}}", "path": "{{.Request.Path}}"}'
```

Start with a minimal set (path, method, query params, path segments, request
body) using Go's `text/template`. Keep it optional — plain strings work exactly
as before.

#### 4. Public API TLS Support

**Benefit:** Medium | **Complexity:** Low

The internal server supports TLS but the public test API does not expose it.
Add:

```go
trenchcoat.WithSelfSignedTLS()  // auto-generate ephemeral cert
trenchcoat.WithTLS(cert, key)   // explicit cert/key files
```

`WithSelfSignedTLS` is the high-value option — generates a self-signed cert at
test startup and returns an `*http.Client` with the right CA pool. Noted in
`test-coverage-analysis.md` as a gap.

---

### Tier 2 — Medium Benefit, Low-Medium Complexity

#### 5. Latency Simulation Profiles

**Benefit:** Medium | **Complexity:** Low

`delay_ms` exists but is a fixed value. Add jitter and distribution options for
more realistic testing:

```yaml
response:
  delay_ms: 100
  delay_jitter_ms: 50    # random ±50ms around 100ms
```

Or at the server level:

```
trenchcoat serve --latency-profile slow  # adds 200-500ms to all responses
```

Useful for testing timeout handling and retry logic.

#### 6. Proxy-to-Coat Workflow Improvements

**Benefit:** Medium | **Complexity:** Low

Small quality-of-life improvements to the proxy capture mode:

- **`--name-template`** — customise captured coat file naming (e.g.
  `{{.Method}}_{{.Path}}_{{.Status}}` instead of the fixed format)
- **`--body-file-threshold`** — automatically write response bodies larger than
  N bytes to separate files using `body_file` instead of inline `body`
- **`--pretty-json`** — pretty-print captured JSON response bodies for
  readability

These are small, independent changes that improve the proxy-to-mock workflow.

#### 7. Conditional Responses (Request-Aware Sequences)

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

#### 8. Import / Compose Coat Files

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

#### 9. Stateful Mock Behaviour

**Benefit:** Medium | **Complexity:** High

Allow coats to define state transitions — e.g. "after POST /users succeeds,
GET /users returns the created user." This is powerful but adds significant
complexity. Consider whether this is better handled by test-level logic using
the programmatic API rather than in coat files.

#### 10. OpenAPI / Swagger Import

**Benefit:** Medium | **Complexity:** High

Generate coat files from an OpenAPI spec. Useful for bootstrapping mocks for
large APIs, but the mapping from schema to realistic response bodies requires
heuristics. Could be a standalone `trenchcoat generate` subcommand.

#### 11. Passthrough Mode (Existing Proposal)

**Benefit:** Medium | **Complexity:** Medium

A hybrid of serve and proxy. Serve matched coats as mocks, but forward
unmatched requests to a real upstream URL. Becomes a `--passthrough
<upstream-url>` flag on the `serve` subcommand.

This is useful for incremental mocking — start by proxying everything, then
gradually replace real calls with coats. The proxy and server infrastructure
already exist, so the main work is the routing logic and merging the two code
paths.

---

### Improvement Ideas for Existing Features

#### A. Better 404 Diagnostics

When no coat matches, the current 404 response only includes method and URI.
Adding a `--verbose` or `--debug` mode that explains *why* each coat didn't
match (e.g. "URI matched but header `Authorization` missing") would
significantly speed up debugging. Could also include a ranked list of
"near-miss" coats.

#### B. Coat Validation Warnings (Non-Fatal)

The `validate` command currently only reports errors. Add warnings for:
- Duplicate coat names (not an error, but likely a mistake)
- Coats that can never match (e.g. shadowed by a more specific coat)
- Unused `body_file` references that don't exist on disk
- Regex patterns that are equivalent to simpler glob patterns

#### C. Request Logging Improvements

The verbose logging is functional but could be richer:
- Log the matched coat's file path and line number
- Log which qualifiers were decisive in matching
- Optional request/response body logging (truncated) for debugging
- Structured log fields for the request body hash (for body-matched coats)

#### D. Glob Pattern Enhancement

The current glob matching uses `path.Match` which only supports `*` (single
path segment) and `?`. It does not support `**` (multi-segment). For URIs like
`/api/v1/users/123/posts/456`, you'd need `/api/v1/users/*/posts/*` — but
`/api/**/posts/*` would be more natural. Consider using `doublestar` or a
custom implementation for `**` support.

---

## Archived / Declined

### ~~Complex Directory Structure~~

Support recursive directory loading with organisational conventions (e.g.
`mocks/users/list.yaml`, `mocks/auth/login.yaml`) and potential shared default
headers/config at directory level. **Declined** — the flat structure with
explicit `--coats` paths is sufficient. The `defaults` block proposal (item 8)
addresses the shared config need without the complexity.

### ~~Request Body Matching~~ (Implemented)

Implemented as exact string matching via the `request.body` field (`*string` —
`nil` means match any body, set value means exact match). Proxy capture
includes `--capture-body` flag (default: `true`). See item 1 for proposed
enhancements (glob, substring, regex body matching).
