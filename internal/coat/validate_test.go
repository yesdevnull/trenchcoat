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

// --- body_match validation ---

func TestValidate_BodyMatchWithoutBody(t *testing.T) {
	f := &File{
		Coats: []Coat{
			{
				Name:     "no-body",
				Request:  Request{URI: "/test", BodyMatch: "glob"},
				Response: &Response{Code: 200},
			},
		},
	}
	errs := Validate(f)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "body_match requires request.body") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error about body_match requiring body, got: %v", errs)
	}
}

func TestValidate_BodyMatchInvalidValue(t *testing.T) {
	body := "hello"
	f := &File{
		Coats: []Coat{
			{
				Name:     "bad-mode",
				Request:  Request{URI: "/test", Body: &body, BodyMatch: "fuzzy"},
				Response: &Response{Code: 200},
			},
		},
	}
	errs := Validate(f)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "must be one of") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error about invalid body_match value, got: %v", errs)
	}
}

func TestValidate_BodyMatchRegexInvalid(t *testing.T) {
	body := "[bad"
	f := &File{
		Coats: []Coat{
			{
				Name:     "bad-regex-body",
				Request:  Request{URI: "/test", Body: &body, BodyMatch: "regex"},
				Response: &Response{Code: 200},
			},
		},
	}
	errs := Validate(f)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "invalid regex") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error about invalid body regex, got: %v", errs)
	}
}

func TestValidate_BodyMatchValid(t *testing.T) {
	body := "test"
	for _, mode := range []string{"exact", "glob", "contains"} {
		f := &File{
			Coats: []Coat{
				{
					Name:     "valid-" + mode,
					Request:  Request{URI: "/test", Body: &body, BodyMatch: mode},
					Response: &Response{Code: 200},
				},
			},
		}
		errs := Validate(f)
		if len(errs) != 0 {
			t.Errorf("body_match=%q: unexpected validation errors: %v", mode, errs)
		}
	}

	// Also test valid regex.
	regexBody := `\d+`
	f := &File{
		Coats: []Coat{
			{
				Name:     "valid-regex",
				Request:  Request{URI: "/test", Body: &regexBody, BodyMatch: "regex"},
				Response: &Response{Code: 200},
			},
		},
	}
	errs := Validate(f)
	if len(errs) != 0 {
		t.Errorf("body_match=regex: unexpected validation errors: %v", errs)
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
	if f.Coats[0].Request.Body == nil || *f.Coats[0].Request.Body != `{"name": "alice"}` {
		t.Fatalf("expected request body %q, got %v", `{"name": "alice"}`, f.Coats[0].Request.Body)
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
	if f.Coats[0].Request.Body == nil || *f.Coats[0].Request.Body != "hello" {
		t.Fatalf("expected request body 'hello', got %v", f.Coats[0].Request.Body)
	}
}
