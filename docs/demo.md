# Trenchcoat Demo

*2026-03-07T11:47:22Z by Showboat dev*
<!-- showboat-id: fcf11ad7-4059-45a7-b630-af2ed6fd6448 -->

Trenchcoat is an extensible mock and proxy-to-mock HTTP server. This demo walks through its key features using the CLI.

## CLI Help

Let's start by exploring the available commands.

```bash
trenchcoat --help
```

```output
Trenchcoat is a CLI tool that serves mock HTTP responses based on configurable request/response definitions called coats.

Usage:
  trenchcoat [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  proxy       Start in proxy capture mode
  serve       Start the mock HTTP server
  validate    Validate one or more coat files for schema correctness

Flags:
      --config string   Path to configuration file
  -h, --help            help for trenchcoat
  -v, --version         version for trenchcoat

Use "trenchcoat [command] --help" for more information about a command.
```

```bash
trenchcoat serve --help
```

```output
Start the mock HTTP server

Usage:
  trenchcoat serve [flags]

Flags:
      --coats strings       Paths to coat files or directories to load
  -h, --help                help for serve
      --log-format string   Log output format: text or json (default "text")
      --port int            Port to listen on (default 8080)
      --tls-cert string     Path to TLS certificate file (PEM)
      --tls-key string      Path to TLS private key file (PEM)
      --verbose             Log each incoming request and match result
      --watch               Watch coat files for changes and hot-reload

Global Flags:
      --config string   Path to configuration file
```

```bash
trenchcoat proxy --help
```

```output
Start in proxy capture mode

Usage:
  trenchcoat proxy <upstream-url> [flags]

Flags:
      --body-file-threshold int   Write response bodies larger than N bytes to separate files (0 = always inline)
      --capture-body              Capture request body in coat files for any request with a body (default true)
      --dedupe string             Deduplication strategy: overwrite, skip, or append (default "overwrite")
      --filter string             Only capture requests whose URI matches this glob pattern
  -h, --help                      help for proxy
      --log-format string         Log output format: text or json (default "text")
      --name-template string      Custom template for captured coat file names (e.g. {{.Method}}-{{.Path}}-{{.Status}})
      --no-headers                Omit all headers from captured coat files (mutually exclusive with --strip-headers)
      --port int                  Port to listen on (default 8080)
      --pretty-json               Pretty-print JSON response bodies in captured coat files
      --strip-headers strings     Headers to redact from captured coat files (default [Authorization,Cookie,Set-Cookie])
      --verbose                   Log each proxied request and capture event
      --write-dir string          Directory to write captured coat files to (default ".")

Global Flags:
      --config string   Path to configuration file
```

```bash
trenchcoat validate --help
```

```output
Validate one or more coat files for schema correctness

Usage:
  trenchcoat validate <path>... [flags]

Flags:
  -h, --help   help for validate

Global Flags:
      --config string   Path to configuration file
```

## Creating and Validating Coat Files

A coat file defines request/response pairs. Let's create a few examples.

```bash
cat basic.yaml
```

```output
coats:
  - name: get-users
    request:
      method: GET
      uri: /api/v1/users
    response:
      code: 200
      headers:
        Content-Type: application/json
      body: '{"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]}'

  - name: create-user
    request:
      method: POST
      uri: /api/v1/users
    response:
      code: 201
      headers:
        Content-Type: application/json
      body: '{"id": 3, "name": "Charlie"}'
```

```bash
trenchcoat validate basic.yaml
```

```output
all coat files are valid
```

## Mock Server

Start the mock server and make some requests.

```bash
trenchcoat serve --coats basic.yaml --port 9100 &
sleep 1

echo "=== GET /api/v1/users ==="
curl -s http://localhost:9100/api/v1/users | jq .

echo ""
echo "=== POST /api/v1/users ==="
curl -s -X POST http://localhost:9100/api/v1/users | jq .

echo ""
echo "=== GET /unknown (no matching coat) ==="
curl -s http://localhost:9100/unknown | jq .

kill %1 2>/dev/null
wait 2>/dev/null
```

```output
time=2026-03-07T11:53:19.479Z level=INFO msg="coats loaded" count=2
time=2026-03-07T11:53:19.480Z level=INFO msg="server started" address=0.0.0.0:9100
=== GET /api/v1/users ===
{
  "users": [
    {
      "id": 1,
      "name": "Alice"
    },
    {
      "id": 2,
      "name": "Bob"
    }
  ]
}

=== POST /api/v1/users ===
{
  "id": 3,
  "name": "Charlie"
}

=== GET /unknown (no matching coat) ===
{
  "error": "no matching coat",
  "method": "GET",
  "uri": "/unknown"
}
time=2026-03-07T11:53:20.586Z level=INFO msg="context canceled, shutting down" reason="context canceled"
time=2026-03-07T11:53:20.586Z level=INFO msg="server stopped"
```

## Glob and Regex URI Matching

Trenchcoat supports three URI matching modes: exact, glob (with `*`, `?`, `**`), and regex (prefixed with `~/`).

```bash
cat patterns.yaml
```

```output
coats:
  - name: single-user-glob
    request:
      uri: /api/v1/users/*
    response:
      code: 200
      body: '{"match": "glob-single-segment"}'

  - name: nested-glob
    request:
      uri: /api/**/comments
    response:
      code: 200
      body: '{"match": "glob-multi-segment"}'

  - name: regex-numeric-id
    request:
      uri: ~/api/v1/items/\d+
    response:
      code: 200
      body: '{"match": "regex-numeric"}'
```

```bash
trenchcoat serve --coats patterns.yaml --port 9101 &
sleep 1

echo "=== Glob: /api/v1/users/42 ==="
curl -s http://localhost:9101/api/v1/users/42 | jq .

echo ""
echo "=== Double-star glob: /api/v1/posts/99/comments ==="
curl -s http://localhost:9101/api/v1/posts/99/comments | jq .

echo ""
echo "=== Regex: /api/v1/items/123 ==="
curl -s http://localhost:9101/api/v1/items/123 | jq .

echo ""
echo "=== Regex miss: /api/v1/items/abc (not numeric) ==="
curl -s http://localhost:9101/api/v1/items/abc | jq .

kill %1 2>/dev/null
wait 2>/dev/null
```

```output
time=2026-03-07T11:53:39.168Z level=INFO msg="coats loaded" count=3
time=2026-03-07T11:53:39.169Z level=INFO msg="server started" address=0.0.0.0:9101
=== Glob: /api/v1/users/42 ===
{
  "match": "glob-single-segment"
}

=== Double-star glob: /api/v1/posts/99/comments ===
{
  "match": "glob-multi-segment"
}

=== Regex: /api/v1/items/123 ===
{
  "match": "regex-numeric"
}

=== Regex miss: /api/v1/items/abc (not numeric) ===
{
  "error": "no matching coat",
  "method": "GET",
  "uri": "/api/v1/items/abc"
}
time=2026-03-07T11:53:40.298Z level=INFO msg="context canceled, shutting down" reason="context canceled"
time=2026-03-07T11:53:40.298Z level=INFO msg="server stopped"
```

## Response Sequences

A coat can define multiple responses that cycle through on successive requests. This is useful for simulating retries, eventual consistency, or error recovery.

```bash
cat sequences.yaml
```

```output
coats:
  - name: retry-endpoint
    request:
      uri: /api/v1/health
    responses:
      - code: 503
        body: '{"status": "unavailable"}'
      - code: 503
        body: '{"status": "unavailable"}'
      - code: 200
        body: '{"status": "healthy"}'
    sequence: cycle
```

```bash
trenchcoat serve --coats sequences.yaml --port 9102 &
sleep 1

echo "=== Request 1 (expect 503) ==="
curl -s -w "\nHTTP %{http_code}\n" http://localhost:9102/api/v1/health

echo ""
echo "=== Request 2 (expect 503) ==="
curl -s -w "\nHTTP %{http_code}\n" http://localhost:9102/api/v1/health

echo ""
echo "=== Request 3 (expect 200) ==="
curl -s -w "\nHTTP %{http_code}\n" http://localhost:9102/api/v1/health

echo ""
echo "=== Request 4 (cycles back to 503) ==="
curl -s -w "\nHTTP %{http_code}\n" http://localhost:9102/api/v1/health

kill %1 2>/dev/null
wait 2>/dev/null
```

```output
time=2026-03-07T11:53:57.795Z level=INFO msg="coats loaded" count=1
time=2026-03-07T11:53:57.796Z level=INFO msg="server started" address=0.0.0.0:9102
=== Request 1 (expect 503) ===
{"status": "unavailable"}
HTTP 503

=== Request 2 (expect 503) ===
{"status": "unavailable"}
HTTP 503

=== Request 3 (expect 200) ===
{"status": "healthy"}
HTTP 200

=== Request 4 (cycles back to 503) ===
{"status": "unavailable"}
HTTP 503
time=2026-03-07T11:53:59.082Z level=INFO msg="context canceled, shutting down" reason="context canceled"
time=2026-03-07T11:53:59.083Z level=INFO msg="server stopped"
```

## Header Matching

Coats can require specific headers, with glob pattern support for values.

```bash
cat headers.yaml
```

```output
coats:
  - name: authorized
    request:
      uri: /api/v1/secret
      headers:
        Authorization: "Bearer *"
    response:
      code: 200
      body: '{"secret": "treasure"}'

  - name: unauthorized
    request:
      uri: /api/v1/secret
    response:
      code: 401
      body: '{"error": "missing auth"}'
```

```bash
trenchcoat serve --coats headers.yaml --port 9103 &
sleep 1

echo "=== With Authorization header ==="
curl -s -H "Authorization: Bearer my-token" http://localhost:9103/api/v1/secret | jq .

echo ""
echo "=== Without Authorization header ==="
curl -s http://localhost:9103/api/v1/secret | jq .

kill %1 2>/dev/null
wait 2>/dev/null
```

```output
time=2026-03-07T11:54:14.389Z level=INFO msg="coats loaded" count=2
time=2026-03-07T11:54:14.390Z level=INFO msg="server started" address=0.0.0.0:9103
=== With Authorization header ===
{
  "secret": "treasure"
}

=== Without Authorization header ===
{
  "error": "missing auth"
}
time=2026-03-07T11:54:15.454Z level=INFO msg="context canceled, shutting down" reason="context canceled"
time=2026-03-07T11:54:15.455Z level=INFO msg="server stopped"
```

## Validation Errors

The `validate` command catches schema errors before you start the server.

```bash
cat invalid.yaml
```

```output
coats:
  - name: broken
    request:
      uri: ""
    response:
      code: 200
      body: inline
      body_file: also-a-file.json
  - name: also-broken
    request:
      uri: /test
    response:
      code: 200
    responses:
      - code: 200
```

```bash
trenchcoat validate invalid.yaml || true
```

```output
error: invalid.yaml: broken: request.uri is required
error: invalid.yaml: broken: response: 'body' and 'body_file' are mutually exclusive
error: invalid.yaml: also-broken: coat must have either 'response' or 'responses', not both
error: validation failed with 3 error(s)
```

## Proxy Capture

Trenchcoat can act as a proxy, capturing real request/response pairs as coat files. Here we proxy to our own mock server to demonstrate the flow.

```bash
mkdir -p captured
trenchcoat serve --coats basic.yaml --port 9106 &
sleep 1
trenchcoat proxy http://localhost:9106 --port 9107 --write-dir captured --pretty-json &
sleep 1
curl -s http://localhost:9107/api/v1/users > /dev/null
curl -s -X POST http://localhost:9107/api/v1/users > /dev/null
sleep 1
echo "=== Captured coat files ==="
ls captured/
echo ""
echo "=== Contents of captured GET coat ==="
cat captured/GET_api_v1_users_200.yaml
kill %2 %1 2>/dev/null; wait 2>/dev/null
```

```output
time=2026-03-07T11:56:48.506Z level=INFO msg="coats loaded" count=2
time=2026-03-07T11:56:48.507Z level=INFO msg="server started" address=0.0.0.0:9106
time=2026-03-07T11:56:49.517Z level=INFO msg="proxy started" address=0.0.0.0:9107 upstream=http://localhost:9106 write_dir=captured filter="" dedupe=overwrite
=== Captured coat files ===
GET_api_v1_users_200.yaml
POST_api_v1_users_201.yaml

=== Contents of captured GET coat ===
coats:
    - name: GET /api/v1/users
      request:
        method: GET
        uri: /api/v1/users
        headers:
            Accept: '*/*'
            User-Agent: curl/8.5.0
      response:
        code: 200
        headers:
            Content-Length: "65"
            Content-Type: application/json
            Date: Sat, 07 Mar 2026 11:56:50 GMT
        body: |-
            {
              "users": [
                {
                  "id": 1,
                  "name": "Alice"
                },
                {
                  "id": 2,
                  "name": "Bob"
                }
              ]
            }
time=2026-03-07T11:56:51.615Z level=INFO msg="context canceled, shutting down" reason="context canceled"
time=2026-03-07T11:56:51.615Z level=INFO msg="context canceled, shutting down" reason="context canceled"
time=2026-03-07T11:56:51.615Z level=INFO msg="proxy stopped"
time=2026-03-07T11:56:51.615Z level=INFO msg="server stopped"
```
