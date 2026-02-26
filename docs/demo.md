# Trenchcoat Demo

*2026-02-26T11:53:38Z by Showboat dev*
<!-- showboat-id: b972c752-4b82-4dee-8822-61deeb51abf9 -->

Trenchcoat is an extensible mock, and proxy-to-mock, HTTP server written in Go. This demo walks through its core features.

## Help Output

Let's see the help output for each subcommand.

```bash
./trenchcoat --help
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
./trenchcoat serve --help
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
      --tls-ca string       Path to CA certificate chain file (PEM)
      --tls-cert string     Path to TLS certificate file (PEM)
      --tls-key string      Path to TLS private key file (PEM)
      --verbose             Log each incoming request and match result
      --watch               Watch coat files for changes and hot-reload

Global Flags:
      --config string   Path to configuration file
```

```bash
./trenchcoat proxy --help
```

```output
Start in proxy capture mode

Usage:
  trenchcoat proxy <upstream-url> [flags]

Flags:
      --dedupe string           Deduplication strategy: overwrite, skip, or append (default "overwrite")
      --filter string           Only capture requests whose URI matches this glob pattern
  -h, --help                    help for proxy
      --log-format string       Log output format: text or json (default "text")
      --port int                Port to listen on (default 8080)
      --strip-headers strings   Headers to redact from captured coat files (default [Authorization,Cookie,Set-Cookie])
      --tls-ca string           Path to CA certificate chain file (PEM)
      --tls-cert string         Path to TLS certificate file (PEM)
      --tls-key string          Path to TLS private key file (PEM)
      --verbose                 Log each proxied request and capture event
      --write-dir string        Directory to write captured coat files to (default ".")

Global Flags:
      --config string   Path to configuration file
```

```bash
./trenchcoat validate --help
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

Let's create some example coat files and validate them.

```bash
mkdir -p /tmp/trenchcoat-demo/mocks && cat > /tmp/trenchcoat-demo/mocks/users.yaml << 'EOF'
coats:
  - name: "get-users"
    request:
      method: GET
      uri: "/api/v1/users"
    response:
      code: 200
      headers:
        Content-Type: "application/json"
      body: |
        {"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]}

  - name: "create-user"
    request:
      method: POST
      uri: "/api/v1/users"
    response:
      code: 201
      headers:
        Content-Type: "application/json"
        Location: "/api/v1/users/3"
      body: |
        {"id": 3, "name": "Charlie"}
EOF
echo "Created users.yaml"
```

```output
Created users.yaml
```

```bash
./trenchcoat validate /tmp/trenchcoat-demo/mocks/users.yaml
```

```output
all coat files are valid
```

## Validation Failure

Now let's see what happens when a coat file has errors — both `response` and `responses` set, and a missing URI.

```bash
./trenchcoat validate /tmp/trenchcoat-demo/mocks/bad.yaml; echo "Exit code: $?"
```

```output
error: /tmp/trenchcoat-demo/mocks/bad.yaml: broken-both: coat must have either 'response' or 'responses', not both
error: /tmp/trenchcoat-demo/mocks/bad.yaml: no-uri: request.uri is required
error: validation failed with 2 error(s)
Exit code: 1
```

## Serving Mock Responses

Start the mock server and make requests with curl.

```bash
./trenchcoat serve --coats /tmp/trenchcoat-demo/mocks/users.yaml --port 19876 &
sleep 0.5
curl -s http://127.0.0.1:19876/api/v1/users | python3 -m json.tool
echo "---"
curl -s -X POST http://127.0.0.1:19876/api/v1/users | python3 -m json.tool
echo "---"
curl -s http://127.0.0.1:19876/not/found | python3 -m json.tool
kill %1 2>/dev/null; wait 2>/dev/null
```

```output
time=2026-02-26T11:55:34.290Z level=INFO msg="coats loaded" count=2
time=2026-02-26T11:55:34.291Z level=INFO msg="server started" address=0.0.0.0:19876
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
---
{
    "id": 3,
    "name": "Charlie"
}
---
{
    "error": "no matching coat",
    "method": "GET",
    "uri": "/not/found"
}
time=2026-02-26T11:55:35.118Z level=INFO msg="received signal, shutting down" signal=terminated
time=2026-02-26T11:55:35.119Z level=INFO msg="server stopped"
```

## Glob and Regex URI Matching

Create coats with glob and regex URI patterns, then test them.

```bash
./trenchcoat serve --coats /tmp/trenchcoat-demo/mocks/patterns.yaml --port 19877 &
sleep 0.5
echo "Glob match /api/users/42:"
curl -s http://127.0.0.1:19877/api/users/42
echo ""
echo "Glob match /api/users/alice:"
curl -s http://127.0.0.1:19877/api/users/alice
echo ""
echo "Regex match /api/items/123:"
curl -s http://127.0.0.1:19877/api/items/123
echo ""
echo "Regex NO match /api/items/abc:"
curl -s http://127.0.0.1:19877/api/items/abc
echo ""
kill %1 2>/dev/null; wait 2>/dev/null
```

```output
time=2026-02-26T11:55:58.101Z level=INFO msg="coats loaded" count=2
time=2026-02-26T11:55:58.102Z level=INFO msg="server started" address=0.0.0.0:19877
Glob match /api/users/42:
{"match": "glob", "pattern": "/api/users/*"}
Glob match /api/users/alice:
{"match": "glob", "pattern": "/api/users/*"}
Regex match /api/items/123:
{"match": "regex", "pattern": "/api/items/\\d+"}
Regex NO match /api/items/abc:
{"error":"no matching coat","method":"GET","uri":"/api/items/abc"}

time=2026-02-26T11:55:58.736Z level=INFO msg="received signal, shutting down" signal=terminated
time=2026-02-26T11:55:58.736Z level=INFO msg="server stopped"
```

## Response Sequences

Coats can define a sequence of responses. In cycle mode, responses rotate. Here a health endpoint returns 503 twice before succeeding, then cycles.

```bash
./trenchcoat serve --coats /tmp/trenchcoat-demo/mocks/sequence.yaml --port 19878 &
sleep 0.5
echo "Request 1: $(curl -s -w "%{http_code}" http://127.0.0.1:19878/health)"
echo "Request 2: $(curl -s -w "%{http_code}" http://127.0.0.1:19878/health)"
echo "Request 3: $(curl -s -w "%{http_code}" http://127.0.0.1:19878/health)"
echo "Request 4: $(curl -s -w "%{http_code}" http://127.0.0.1:19878/health)"
kill %1 2>/dev/null; wait 2>/dev/null
```

```output
time=2026-02-26T11:56:36.397Z level=INFO msg="coats loaded" count=1
time=2026-02-26T11:56:36.398Z level=INFO msg="server started" address=0.0.0.0:19878
Request 1: Service Unavailable503
Request 2: Service Unavailable503
Request 3: {"status": "ok"}200
Request 4: Service Unavailable503
time=2026-02-26T11:56:37.057Z level=INFO msg="received signal, shutting down" signal=terminated
time=2026-02-26T11:56:37.057Z level=INFO msg="server stopped"
```

## Header Matching

Coats can require specific headers (with glob support). This coat only matches when an Authorization header with a Bearer token is present.

```bash
./trenchcoat serve --coats /tmp/trenchcoat-demo/mocks/headers.yaml --port 19879 &
sleep 0.5
echo "With Authorization header:"
curl -s -H "Authorization: Bearer my-token" http://127.0.0.1:19879/api/protected
echo ""
echo "Without Authorization header:"
curl -s http://127.0.0.1:19879/api/protected
echo ""
kill %1 2>/dev/null; wait 2>/dev/null
```

```output
time=2026-02-26T11:57:00.224Z level=INFO msg="coats loaded" count=1
time=2026-02-26T11:57:00.225Z level=INFO msg="server started" address=0.0.0.0:19879
With Authorization header:
{"message": "welcome, authorised user"}
Without Authorization header:
{"error":"no matching coat","method":"GET","uri":"/api/protected"}

time=2026-02-26T11:57:00.807Z level=INFO msg="received signal, shutting down" signal=terminated
time=2026-02-26T11:57:00.807Z level=INFO msg="server stopped"
```

## Proxy Capture Mode

Proxy mode forwards requests to an upstream server while capturing request/response pairs as coat files. Here we use trenchcoat serve as the upstream and proxy through to it.

```bash
rm -rf /tmp/trenchcoat-demo/captured

# Start upstream mock
./trenchcoat serve --coats /tmp/trenchcoat-demo/mocks/users.yaml --port 19882 &
sleep 0.3

# Start proxy
./trenchcoat proxy http://127.0.0.1:19882 --port 19883 --write-dir /tmp/trenchcoat-demo/captured &
sleep 0.3

# Make a request through the proxy
echo "Response through proxy:"
curl -s http://127.0.0.1:19883/api/v1/users
echo ""
sleep 0.3

# Show the captured coat file
echo "Captured coat file:"
cat /tmp/trenchcoat-demo/captured/*.yaml

kill %2 %1 2>/dev/null
wait 2>/dev/null
```

```output
time=2026-02-26T12:00:22.269Z level=INFO msg="coats loaded" count=2
time=2026-02-26T12:00:22.270Z level=INFO msg="server started" address=0.0.0.0:19882
time=2026-02-26T12:00:22.581Z level=INFO msg="proxy started" address=0.0.0.0:19883 upstream=http://127.0.0.1:19882 write_dir=/tmp/trenchcoat-demo/captured filter="" dedupe=overwrite
Response through proxy:
{"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]}

Captured coat file:
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
            Content-Length: "66"
            Content-Type: application/json
            Date: Thu, 26 Feb 2026 12:00:22 GMT
        body: |
            {"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]}
time=2026-02-26T12:00:23.228Z level=INFO msg="received signal, shutting down" signal=terminated
time=2026-02-26T12:00:23.228Z level=INFO msg="received signal, shutting down" signal=terminated
time=2026-02-26T12:00:23.229Z level=INFO msg="proxy stopped"
time=2026-02-26T12:00:23.229Z level=INFO msg="server stopped"
```
