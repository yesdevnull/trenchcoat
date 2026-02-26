package coat_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yesdevnull/trenchcoat/internal/coat"
)

// --- YAML parsing tests ---

func TestParseYAML_BasicCoat(t *testing.T) {
	yaml := `
coats:
  - name: "get-users"
    request:
      method: GET
      uri: "/api/v1/users"
    response:
      code: 200
      headers:
        Content-Type: "application/json"
      body: '{"users": []}'
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coats.Coats) != 1 {
		t.Fatalf("expected 1 coat, got %d", len(coats.Coats))
	}
	c := coats.Coats[0]
	assertEqual(t, "name", "get-users", c.Name)
	assertEqual(t, "method", "GET", c.Request.Method)
	assertEqual(t, "uri", "/api/v1/users", c.Request.URI)
	assertEqual(t, "code", 200, c.Response.Code)
	assertEqual(t, "content-type", "application/json", c.Response.Headers["Content-Type"])
	assertEqual(t, "body", `{"users": []}`, c.Response.Body)
}

func TestParseYAML_QueryAsString(t *testing.T) {
	yaml := `
coats:
  - name: "query-string"
    request:
      uri: "/search"
      query: "page=1&limit=10"
    response:
      code: 200
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := coats.Coats[0].Request.Query
	if q == nil {
		t.Fatal("expected query to be set")
	}
	if q.Raw != "page=1&limit=10" {
		t.Fatalf("expected raw query 'page=1&limit=10', got %q", q.Raw)
	}
	if q.Map != nil {
		t.Fatalf("expected query map to be nil, got %v", q.Map)
	}
}

func TestParseYAML_QueryAsMap(t *testing.T) {
	yaml := `
coats:
  - name: "query-map"
    request:
      uri: "/search"
      query:
        page: "1"
        limit: "*"
    response:
      code: 200
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := coats.Coats[0].Request.Query
	if q == nil {
		t.Fatal("expected query to be set")
	}
	if q.Raw != "" {
		t.Fatalf("expected empty raw query, got %q", q.Raw)
	}
	assertEqual(t, "page", "1", q.Map["page"])
	assertEqual(t, "limit", "*", q.Map["limit"])
}

func TestParseYAML_ResponsesPlural(t *testing.T) {
	yaml := `
coats:
  - name: "sequence"
    request:
      uri: "/health"
    responses:
      - code: 503
        body: "Service Unavailable"
      - code: 200
        body: '{"status": "ok"}'
    sequence: cycle
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := coats.Coats[0]
	if c.Response != nil {
		t.Fatal("expected singular response to be nil")
	}
	if len(c.Responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(c.Responses))
	}
	assertEqual(t, "sequence", "cycle", c.Sequence)
	assertEqual(t, "first code", 503, c.Responses[0].Code)
	assertEqual(t, "second code", 200, c.Responses[1].Code)
}

func TestParseYAML_DefaultMethod(t *testing.T) {
	yaml := `
coats:
  - request:
      uri: "/test"
    response:
      code: 200
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Method should default to empty string at parse level; defaults are applied by the matching engine.
	// But at the parse level we just confirm it parsed correctly.
	c := coats.Coats[0]
	assertEqual(t, "uri", "/test", c.Request.URI)
}

// --- JSON parsing tests ---

func TestParseJSON_BasicCoat(t *testing.T) {
	json := `{
  "coats": [
    {
      "name": "get-users",
      "request": {
        "method": "GET",
        "uri": "/api/v1/users"
      },
      "response": {
        "code": 200,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"users\": []}"
      }
    }
  ]
}`
	f := writeTemp(t, "coat.json", json)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coats.Coats) != 1 {
		t.Fatalf("expected 1 coat, got %d", len(coats.Coats))
	}
	c := coats.Coats[0]
	assertEqual(t, "name", "get-users", c.Name)
	assertEqual(t, "code", 200, c.Response.Code)
}

func TestParseJSON_QueryAsString(t *testing.T) {
	json := `{
  "coats": [{
    "request": {
      "uri": "/search",
      "query": "page=1"
    },
    "response": {"code": 200}
  }]
}`
	f := writeTemp(t, "coat.json", json)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := coats.Coats[0].Request.Query
	if q == nil {
		t.Fatal("expected query to be set")
	}
	assertEqual(t, "raw", "page=1", q.Raw)
}

func TestParseJSON_QueryAsMap(t *testing.T) {
	json := `{
  "coats": [{
    "request": {
      "uri": "/search",
      "query": {"page": "1", "limit": "*"}
    },
    "response": {"code": 200}
  }]
}`
	f := writeTemp(t, "coat.json", json)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := coats.Coats[0].Request.Query
	if q == nil {
		t.Fatal("expected query to be set")
	}
	assertEqual(t, "page", "1", q.Map["page"])
	assertEqual(t, "limit", "*", q.Map["limit"])
}

// --- File extension handling ---

func TestParseFile_UnknownExtension(t *testing.T) {
	f := writeTemp(t, "coat.txt", "some content")
	_, err := coat.ParseFile(f)
	if err == nil {
		t.Fatal("expected error for unknown extension")
	}
}

func TestParseFile_YMLExtension(t *testing.T) {
	yaml := `
coats:
  - request:
      uri: "/test"
    response:
      code: 200
`
	f := writeTemp(t, "coat.yml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coats.Coats) != 1 {
		t.Fatalf("expected 1 coat, got %d", len(coats.Coats))
	}
}

// --- Validation tests ---

func TestValidate_MissingURI(t *testing.T) {
	yaml := `
coats:
  - name: "no-uri"
    request:
      method: GET
    response:
      code: 200
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for missing URI")
	}
}

func TestValidate_BothResponseAndResponses(t *testing.T) {
	yaml := `
coats:
  - name: "both"
    request:
      uri: "/test"
    response:
      code: 200
    responses:
      - code: 200
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for both response and responses")
	}
}

func TestValidate_NeitherResponseNorResponses(t *testing.T) {
	yaml := `
coats:
  - name: "neither"
    request:
      uri: "/test"
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for no response")
	}
}

func TestValidate_BodyAndBodyFileMutuallyExclusive(t *testing.T) {
	yaml := `
coats:
  - name: "both-body"
    request:
      uri: "/test"
    response:
      code: 200
      body: "hello"
      body_file: "file.json"
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for body and body_file")
	}
}

func TestValidate_BodyAndBodyFileInResponses(t *testing.T) {
	yaml := `
coats:
  - name: "both-in-responses"
    request:
      uri: "/test"
    responses:
      - code: 200
        body: "hello"
        body_file: "file.json"
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for body and body_file in responses")
	}
}

func TestValidate_SequenceWithSingularResponse(t *testing.T) {
	yaml := `
coats:
  - name: "bad-sequence"
    request:
      uri: "/test"
    response:
      code: 200
    sequence: cycle
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for sequence with singular response")
	}
}

func TestValidate_ValidSequence(t *testing.T) {
	yaml := `
coats:
  - name: "ok-sequence"
    request:
      uri: "/test"
    responses:
      - code: 200
      - code: 503
    sequence: once
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestValidate_InvalidSequenceValue(t *testing.T) {
	yaml := `
coats:
  - name: "bad-value"
    request:
      uri: "/test"
    responses:
      - code: 200
    sequence: random
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) == 0 {
		t.Fatal("expected validation error for invalid sequence value")
	}
}

func TestValidate_ValidCoat(t *testing.T) {
	yaml := `
coats:
  - name: "good-coat"
    request:
      method: POST
      uri: "/api/users"
      headers:
        Content-Type: "application/json"
      query:
        page: "1"
    response:
      code: 201
      headers:
        Location: "/api/users/1"
      body: '{"id": 1}'
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestValidate_MultipleCoats_OneInvalid(t *testing.T) {
	yaml := `
coats:
  - name: "valid"
    request:
      uri: "/ok"
    response:
      code: 200
  - name: "invalid"
    request:
      method: GET
    response:
      code: 200
`
	f := writeTemp(t, "coat.yaml", yaml)
	coats, err := coat.ParseFile(f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	errs := coat.Validate(coats)
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
}

// --- LoadDirectory tests ---

func TestLoadDirectory(t *testing.T) {
	dir := t.TempDir()
	yaml1 := `
coats:
  - request:
      uri: "/a"
    response:
      code: 200
`
	yaml2 := `
coats:
  - request:
      uri: "/b"
    response:
      code: 201
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(yaml1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yml"), []byte(yaml2), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := coat.LoadDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 coat files, got %d", len(files))
	}
}

// --- Helpers ---

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}

func assertEqual[T comparable](t *testing.T, field string, expected, actual T) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", field, expected, actual)
	}
}
