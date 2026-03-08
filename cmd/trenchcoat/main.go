package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/yesdevnull/trenchcoat/internal/config"
)

// Version information set at build time via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const longHelp = `Trenchcoat — extensible mock HTTP server and proxy-to-mock capture tool.

COMMANDS
  trenchcoat serve       Start a mock server from coat files
  trenchcoat proxy       Capture live traffic as coat files
  trenchcoat validate    Check coat files for errors

QUICK START

  1. Create a coat file (YAML or JSON) defining request/response mocks:

     # hello.yaml
     coats:
       - name: hello
         request:
           uri: /hello
         response:
           code: 200
           headers:
             Content-Type: application/json
           body: '{"message": "world"}'

  2. Start the server:

     trenchcoat serve --coats hello.yaml --port 9090

  3. Test it:

     curl http://localhost:9090/hello

  To generate coats from a live API instead of writing them by hand:

     trenchcoat proxy https://api.example.com --write-dir ./coats --pretty-json

COAT FILE FORMAT

  Coat files are YAML or JSON. Each file has a top-level "coats" array.
  Every coat has a "request" matcher and either "response" (singular) or
  "responses" (plural, for sequences). Minimal example:

     coats:
       - request:
           uri: /path
         response:
           code: 200

  Request fields:
    uri          (required) URI to match. Plain string, glob (*/?, ** for
                 multi-segment), or regex (~/pattern).
    method       HTTP method (default GET). Use ANY to match all methods.
    headers      Map of header name to value. Values support glob patterns.
    query        Raw query string or map of key/value pairs (glob values).
    body         String to match against request body.
    body_match   Matching mode for body: exact (default), glob, contains, regex.

  Response fields:
    code             HTTP status code (default 200).
    headers          Map of response headers.
    body             Response body string.
    body_file        Path to file for response body (relative to coat file).
                     Mutually exclusive with body.
    delay_ms         Milliseconds to wait before responding.
    delay_jitter_ms  Random jitter (0 to N ms) added on top of delay_ms.

  Response body templates — if the body contains "{{", it is rendered as a
  Go text/template with these fields:
    {{.Method}}      HTTP method
    {{.Path}}        Request path
    {{.Body}}        Request body
    {{.Query "key"}} Query parameter value
    {{.Segment N}}   Nth path segment (0-indexed)

  Sequences — use "responses" (plural) for ordered responses:

     coats:
       - name: retry-then-ok
         request:
           uri: /health
         responses:
           - code: 503
             body: unavailable
           - code: 200
             body: ok
         sequence: cycle    # cycle (default) or once

  Variable substitution — ${VAR} and ${VAR:-default} in coat values are
  resolved from environment variables at parse time.

URI MATCHING (highest to lowest precedence)
  1. Exact string         /api/v1/users
  2. Glob pattern          /api/v1/users/* or /api/**/posts
  3. Regex (~/prefix)     ~/api/v1/users/\d+
  Method-specific coats rank above method: ANY at the same specificity.

SERVE FLAGS
  --coats strings       Coat files or directories to load (repeatable)
  --port int            Listen port (default 8080)
  --tls-cert string     TLS certificate PEM (requires --tls-key)
  --tls-key string      TLS private key PEM (requires --tls-cert)
  --watch               Hot-reload when coat files change on disk
  --verbose             Log each request with match details and near-misses
  --log-format string   text (default) or json

PROXY FLAGS
  trenchcoat proxy <upstream-url>
  --port int                Listen port (default 8080)
  --write-dir string        Output directory for captured coats (default ".")
  --filter string           Glob to limit which URIs are captured
  --strip-headers strings   Headers to redact (default: Authorization,Cookie,Set-Cookie)
  --no-headers              Omit all headers (mutually exclusive with --strip-headers)
  --dedupe string           overwrite (default), skip, or append
  --capture-body            Include request body in captures (default true)
  --pretty-json             Pretty-print JSON response bodies
  --body-file-threshold int Write bodies larger than N bytes to separate files
  --name-template string    Custom filename template, e.g. {{.Method}}-{{.Path}}-{{.Status}}

VALIDATE
  trenchcoat validate <path>...
  Exits 0 if all files are valid. Prints warnings for duplicate coat names
  and regex patterns that could be simpler globs.

CONFIGURATION
  Flags can also be set in a config file (YAML). Discovery order:
    1. --config flag
    2. .trenchcoat.yaml in the current directory
    3. ~/.config/trenchcoat/config.yaml

EXAMPLES

  Mock a REST API with multiple endpoints:

    # api.yaml
    coats:
      - name: list-users
        request:
          method: GET
          uri: /api/users
        response:
          code: 200
          headers:
            Content-Type: application/json
          body: '[{"id": 1, "name": "Alice"}]'

      - name: get-user
        request:
          method: GET
          uri: "~/api/users/\\d+"
        response:
          code: 200
          body: '{"id": {{.Segment 2}}, "name": "Alice"}'

      - name: create-user
        request:
          method: POST
          uri: /api/users
          headers:
            Content-Type: application/json
        response:
          code: 201
          body: '{"id": 2, "name": "created"}'

    trenchcoat serve --coats api.yaml --verbose

  Capture from a live API, then serve the captures:

    trenchcoat proxy https://api.example.com \
      --write-dir ./captured \
      --pretty-json \
      --verbose

    # (make requests through http://localhost:8080)
    # then serve the captured coats:

    trenchcoat serve --coats ./captured --port 9090

  Validate coat files in CI:

    trenchcoat validate ./coats/`

func main() {
	rootCmd := &cobra.Command{
		Use:     "trenchcoat",
		Short:   "Extensible mock, and proxy-to-mock, HTTP server",
		Long:    longHelp,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfgFile, _ := cmd.Flags().GetString("config")
			return config.Load(cfgFile)
		},
	}

	rootCmd.PersistentFlags().String("config", "", "Path to configuration file")

	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newProxyCmd())

	// Set up signal-based context so serve/proxy commands shut down on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
