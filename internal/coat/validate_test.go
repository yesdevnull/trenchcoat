package coat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidationError_Error_WithName(t *testing.T) {
	e := &ValidationError{CoatIndex: 0, CoatName: "my-coat", Message: "bad field"}
	got := e.Error()
	if got != "my-coat: bad field" {
		t.Fatalf("expected 'my-coat: bad field', got %q", got)
	}
}

func TestValidationError_Error_WithoutName(t *testing.T) {
	e := &ValidationError{CoatIndex: 3, CoatName: "", Message: "missing uri"}
	got := e.Error()
	if got != "coat[3]: missing uri" {
		t.Fatalf("expected 'coat[3]: missing uri', got %q", got)
	}
}

func TestValidate_InvalidRegexURI(t *testing.T) {
	f := &File{
		Coats: []Coat{
			{
				Name:     "bad-regex",
				Request:  Request{URI: "~/api/[bad"},
				Response: &Response{Code: 200},
			},
		},
	}
	errs := Validate(f)
	if len(errs) == 0 {
		t.Fatal("expected validation error for invalid regex")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "invalid regex") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'invalid regex' error, got: %v", errs)
	}
}

func TestParseYAML_MalformedSyntax(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.yaml"
	if err := os.WriteFile(path, []byte(":\n  - [bad yaml content"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "parsing YAML") {
		t.Fatalf("expected YAML parsing error, got: %v", err)
	}
}

func TestParseJSON_MalformedSyntax(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.json"
	if err := os.WriteFile(path, []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parsing JSON") {
		t.Fatalf("expected JSON parsing error, got: %v", err)
	}
}

func TestParseFile_NonExistent(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "reading coat file") {
		t.Fatalf("expected 'reading coat file' error, got: %v", err)
	}
}

func TestParseFile_NonExistentJSON(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for non-existent JSON file")
	}
	if !strings.Contains(err.Error(), "reading coat file") {
		t.Fatalf("expected 'reading coat file' error, got: %v", err)
	}
}

func TestQueryField_UnmarshalJSON_InvalidType(t *testing.T) {
	var q QueryField
	err := q.UnmarshalJSON([]byte(`[1, 2, 3]`))
	if err == nil {
		t.Fatal("expected error for JSON array query")
	}
	if !strings.Contains(err.Error(), "expected string or object") {
		t.Fatalf("expected 'expected string or object' error, got: %v", err)
	}
}

func TestParseYAML_RequestBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.yaml")
	content := `coats:
  - name: post-with-body
    request:
      method: POST
      uri: /api/v1/users
      body: '{"name": "alice"}'
    response:
      code: 201
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if len(f.Coats) != 1 {
		t.Fatalf("expected 1 coat, got %d", len(f.Coats))
	}
	if f.Coats[0].Request.Body != `{"name": "alice"}` {
		t.Fatalf("expected request body, got %q", f.Coats[0].Request.Body)
	}
}

func TestParseJSON_RequestBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	content := `{"coats": [{"name": "post-json", "request": {"method": "POST", "uri": "/api", "body": "hello"}, "response": {"code": 200}}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if f.Coats[0].Request.Body != "hello" {
		t.Fatalf("expected request body 'hello', got %q", f.Coats[0].Request.Body)
	}
}
