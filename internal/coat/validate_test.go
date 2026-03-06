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

// --- Variable substitution ---

func TestSubstituteVars_EnvVar(t *testing.T) {
	t.Setenv("TRENCHCOAT_TEST_HOST", "api.example.com")

	dir := t.TempDir()
	path := filepath.Join(dir, "vars.yaml")
	content := `coats:
  - name: env-test
    request:
      uri: "/${TRENCHCOAT_TEST_HOST}/users"
    response:
      code: 200
      body: '{"host": "${TRENCHCOAT_TEST_HOST}"}'
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if f.Coats[0].Request.URI != "/api.example.com/users" {
		t.Errorf("expected URI '/api.example.com/users', got %q", f.Coats[0].Request.URI)
	}
	if f.Coats[0].Response.Body != `{"host": "api.example.com"}` {
		t.Errorf("expected body with substituted host, got %q", f.Coats[0].Response.Body)
	}
}

func TestSubstituteVars_Default(t *testing.T) {
	// Ensure the variable is NOT set.
	t.Setenv("TRENCHCOAT_UNSET_VAR", "")
	os.Unsetenv("TRENCHCOAT_UNSET_VAR")

	dir := t.TempDir()
	path := filepath.Join(dir, "defaults.yaml")
	content := `coats:
  - name: default-test
    request:
      uri: "${TRENCHCOAT_UNSET_VAR:-/api/v1}/users"
    response:
      code: 200
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if f.Coats[0].Request.URI != "/api/v1/users" {
		t.Errorf("expected URI '/api/v1/users', got %q", f.Coats[0].Request.URI)
	}
}

func TestSubstituteVars_EnvOverridesDefault(t *testing.T) {
	t.Setenv("TRENCHCOAT_PREFIX", "/api/v2")

	dir := t.TempDir()
	path := filepath.Join(dir, "override.yaml")
	content := `coats:
  - name: override-test
    request:
      uri: "${TRENCHCOAT_PREFIX:-/api/v1}/users"
    response:
      code: 200
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if f.Coats[0].Request.URI != "/api/v2/users" {
		t.Errorf("expected URI '/api/v2/users', got %q", f.Coats[0].Request.URI)
	}
}

func TestSubstituteVars_UnsetNoDefault_LeftAsIs(t *testing.T) {
	os.Unsetenv("TRENCHCOAT_NONEXISTENT_AAAA")

	dir := t.TempDir()
	path := filepath.Join(dir, "noop.yaml")
	content := `coats:
  - name: noop-test
    request:
      uri: "${TRENCHCOAT_NONEXISTENT_AAAA}/users"
    response:
      code: 200
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if f.Coats[0].Request.URI != "${TRENCHCOAT_NONEXISTENT_AAAA}/users" {
		t.Errorf("expected unsubstituted URI, got %q", f.Coats[0].Request.URI)
	}
}

func TestSubstituteVars_JSON(t *testing.T) {
	t.Setenv("TRENCHCOAT_JSON_PORT", "9090")

	dir := t.TempDir()
	path := filepath.Join(dir, "vars.json")
	content := `{"coats": [{"name": "json-var", "request": {"uri": "/api/${TRENCHCOAT_JSON_PORT:-8080}/health"}, "response": {"code": 200}}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := ParseFile(path)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if f.Coats[0].Request.URI != "/api/9090/health" {
		t.Errorf("expected URI '/api/9090/health', got %q", f.Coats[0].Request.URI)
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

// --- Validation Warnings ---

func TestValidateWithWarnings_DuplicateNames(t *testing.T) {
	f := &File{
		Coats: []Coat{
			{
				Name:     "dup",
				Request:  Request{URI: "/a"},
				Response: &Response{Code: 200},
			},
			{
				Name:     "dup",
				Request:  Request{URI: "/b"},
				Response: &Response{Code: 200},
			},
		},
	}
	result := ValidateWithWarnings(f)
	if len(result.Errors) != 0 {
		t.Fatalf("expected no errors, got: %v", result.Errors)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected duplicate name warning")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "duplicate coat name") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'duplicate coat name' warning, got: %v", result.Warnings)
	}
}

func TestValidateWithWarnings_NoDuplicateForDifferentNames(t *testing.T) {
	f := &File{
		Coats: []Coat{
			{
				Name:     "alpha",
				Request:  Request{URI: "/a"},
				Response: &Response{Code: 200},
			},
			{
				Name:     "beta",
				Request:  Request{URI: "/b"},
				Response: &Response{Code: 200},
			},
		},
	}
	result := ValidateWithWarnings(f)
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", result.Warnings)
	}
}

func TestValidateWithWarnings_SimpleRegexWarning(t *testing.T) {
	f := &File{
		Coats: []Coat{
			{
				Name:     "simple-regex",
				Request:  Request{URI: "~/api/v1/users/.*"},
				Response: &Response{Code: 200},
			},
		},
	}
	result := ValidateWithWarnings(f)
	if len(result.Errors) != 0 {
		t.Fatalf("expected no errors, got: %v", result.Errors)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "simpler glob") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'simpler glob' warning for simple regex, got: %v", result.Warnings)
	}
}

func TestValidateWithWarnings_ComplexRegexNoWarning(t *testing.T) {
	f := &File{
		Coats: []Coat{
			{
				Name:     "complex-regex",
				Request:  Request{URI: `~/api/v1/(users|groups)/\d+`},
				Response: &Response{Code: 200},
			},
		},
	}
	result := ValidateWithWarnings(f)
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "simpler glob") {
			t.Fatalf("did not expect glob warning for complex regex, got: %v", w)
		}
	}
}

func TestValidateWithWarnings_WarningString(t *testing.T) {
	w := &ValidationWarning{CoatIndex: 1, CoatName: "my-coat", Message: "something fishy"}
	got := w.String()
	if got != "my-coat: something fishy" {
		t.Fatalf("expected 'my-coat: something fishy', got %q", got)
	}
}

func TestValidateWithWarnings_WarningStringWithoutName(t *testing.T) {
	w := &ValidationWarning{CoatIndex: 2, CoatName: "", Message: "something fishy"}
	got := w.String()
	if got != "coat[2]: something fishy" {
		t.Fatalf("expected 'coat[2]: something fishy', got %q", got)
	}
}

func TestLoadPathsWithWarnings_DuplicateNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dupes.yaml")
	content := `coats:
  - name: dup
    request:
      uri: /a
    response:
      code: 200
  - name: dup
    request:
      uri: /b
    response:
      code: 200
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := LoadPathsWithWarnings([]string{path})
	if len(result.Errors) != 0 {
		t.Fatalf("expected no errors, got: %v", result.Errors)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for duplicate names")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "duplicate coat name") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'duplicate coat name' warning, got: %v", result.Warnings)
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
