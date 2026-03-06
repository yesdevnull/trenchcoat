package trenchcoat

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// httpClient is a shared test client with an explicit timeout to prevent
// tests from hanging indefinitely if the server stalls.
var httpClient = &http.Client{Timeout: 5 * time.Second}

func TestWithCoat(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "test",
			Request:  Request{Method: "GET", URI: "/test"},
			Response: &Response{Code: 200, Body: "hello"},
		}),
	)
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWithCoats(t *testing.T) {
	srv := NewServer(
		WithCoats(
			Coat{
				Name:     "a",
				Request:  Request{Method: "GET", URI: "/a"},
				Response: &Response{Code: 200, Body: "a"},
			},
			Coat{
				Name:     "b",
				Request:  Request{Method: "GET", URI: "/b"},
				Response: &Response{Code: 201, Body: "b"},
			},
		),
	)
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/a")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	resp2, err := httpClient.Get(srv.URL + "/b")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp2.StatusCode)
	}
}

func TestWithCoatFile(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "test.yaml")
	content := `
coats:
  - name: "from-file"
    request:
      method: GET
      uri: "/from-file"
    response:
      code: 200
      body: "loaded from file"
`
	if err := os.WriteFile(coatFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(WithCoatFile(coatFile))
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/from-file")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWithVerbose(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "verbose-test",
			Request:  Request{Method: "GET", URI: "/verbose"},
			Response: &Response{Code: 200, Body: "ok"},
		}),
		WithVerbose(),
	)

	if !srv.verbose {
		t.Fatal("expected verbose to be true")
	}

	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/verbose")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestStop_BeforeStart(t *testing.T) {
	srv := NewServer()
	// Should not panic.
	srv.Stop()
}

func TestNewServer_NoOptions(t *testing.T) {
	srv := NewServer()
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if len(srv.coats) != 0 {
		t.Fatalf("expected 0 coats, got %d", len(srv.coats))
	}
}

func TestWithCoat_BodyMatching(t *testing.T) {
	srv := NewServer(
		WithCoats(
			Coat{
				Name:    "create-alice",
				Request: Request{Method: "POST", URI: "/api/v1/users", Body: StringPtr(`{"name": "alice"}`)},
				Response: &Response{
					Code: 201,
					Body: "alice created",
				},
			},
			Coat{
				Name:    "create-bob",
				Request: Request{Method: "POST", URI: "/api/v1/users", Body: StringPtr(`{"name": "bob"}`)},
				Response: &Response{
					Code: 201,
					Body: "bob created",
				},
			},
		),
	)
	srv.Start(t)

	// POST with alice body.
	resp, err := httpClient.Post(srv.URL+"/api/v1/users", "application/json", strings.NewReader(`{"name": "alice"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if string(body) != "alice created" {
		t.Fatalf("expected 'alice created', got %q", body)
	}

	// POST with bob body.
	resp2, err := httpClient.Post(srv.URL+"/api/v1/users", "application/json", strings.NewReader(`{"name": "bob"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body2, err := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if resp2.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp2.StatusCode)
	}
	if string(body2) != "bob created" {
		t.Fatalf("expected 'bob created', got %q", body2)
	}
}

func TestNewServer_NoCoats_Returns404(t *testing.T) {
	srv := NewServer()
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/anything")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("expected JSON error body, got: %s", body)
	}
	if _, ok := errResp["error"]; !ok {
		t.Fatalf("expected 'error' key in JSON response, got: %s", body)
	}
}

func TestWithCoatFile_NonExistent(t *testing.T) {
	missingCoat := filepath.Join(t.TempDir(), "nonexistent-coat.yaml")
	srv := NewServer(WithCoatFile(missingCoat))
	if len(srv.loadErrs) == 0 {
		t.Fatal("expected load errors for non-existent coat file")
	}
}

// --- Request Assertions / Call Counting ---

func TestAssertCalled(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "get-test",
			Request:  Request{Method: "GET", URI: "/test"},
			Response: &Response{Code: 200, Body: "ok"},
		}),
	)
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	srv.AssertCalled(t, "get-test")
}

func TestAssertCalledN(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "counted",
			Request:  Request{Method: "GET", URI: "/counted"},
			Response: &Response{Code: 200, Body: "ok"},
		}),
	)
	srv.Start(t)

	for range 3 {
		resp, err := httpClient.Get(srv.URL + "/counted")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		_ = resp.Body.Close()
	}

	srv.AssertCalledN(t, "counted", 3)
}

func TestAssertNotCalled(t *testing.T) {
	srv := NewServer(
		WithCoats(
			Coat{
				Name:     "used",
				Request:  Request{Method: "GET", URI: "/used"},
				Response: &Response{Code: 200, Body: "ok"},
			},
			Coat{
				Name:     "unused",
				Request:  Request{Method: "GET", URI: "/unused"},
				Response: &Response{Code: 200, Body: "ok"},
			},
		),
	)
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/used")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	srv.AssertNotCalled(t, "unused")
}

func TestRequests_CapturesDetails(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "capture-test",
			Request:  Request{Method: "POST", URI: "/capture"},
			Response: &Response{Code: 201, Body: "created"},
		}),
	)
	srv.Start(t)

	req, _ := http.NewRequest("POST", srv.URL+"/capture", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("X-Custom", "test-value")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	reqs := srv.Requests("capture-test")
	if len(reqs) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(reqs))
	}
	cr := reqs[0]
	if cr.Method != "POST" {
		t.Errorf("expected method POST, got %q", cr.Method)
	}
	if cr.URI != "/capture" {
		t.Errorf("expected URI /capture, got %q", cr.URI)
	}
	if cr.Header.Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom header 'test-value', got %q", cr.Header.Get("X-Custom"))
	}
	if cr.Body != `{"name":"alice"}` {
		t.Errorf("expected body %q, got %q", `{"name":"alice"}`, cr.Body)
	}
}

func TestResetCalls(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "resettable",
			Request:  Request{Method: "GET", URI: "/reset"},
			Response: &Response{Code: 200, Body: "ok"},
		}),
	)
	srv.Start(t)

	resp, err := httpClient.Get(srv.URL + "/reset")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	srv.AssertCalledN(t, "resettable", 1)
	srv.ResetCalls()
	srv.AssertNotCalled(t, "resettable")
}

// --- Public API TLS Support ---

func TestWithSelfSignedTLS(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "tls-test",
			Request:  Request{Method: "GET", URI: "/secure"},
			Response: &Response{Code: 200, Body: "secure-ok"},
		}),
		WithSelfSignedTLS(),
	)
	srv.Start(t)

	if !strings.HasPrefix(srv.URL, "https://") {
		t.Fatalf("expected https:// URL, got %q", srv.URL)
	}
	if srv.TLSClient == nil {
		t.Fatal("expected TLSClient to be set")
	}

	resp, err := srv.TLSClient.Get(srv.URL + "/secure")
	if err != nil {
		t.Fatalf("TLS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "secure-ok" {
		t.Fatalf("expected 'secure-ok', got %q", body)
	}
}

func TestWithSelfSignedTLS_AssertionsWork(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "tls-assert",
			Request:  Request{Method: "GET", URI: "/check"},
			Response: &Response{Code: 200, Body: "ok"},
		}),
		WithSelfSignedTLS(),
	)
	srv.Start(t)

	resp, err := srv.TLSClient.Get(srv.URL + "/check")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	srv.AssertCalled(t, "tls-assert")
	srv.AssertCalledN(t, "tls-assert", 1)
}

func TestRequests_EmptyForUnknownCoat(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "known",
			Request:  Request{Method: "GET", URI: "/known"},
			Response: &Response{Code: 200, Body: "ok"},
		}),
	)
	srv.Start(t)

	reqs := srv.Requests("nonexistent")
	if len(reqs) != 0 {
		t.Fatalf("expected 0 captured requests for unknown coat, got %d", len(reqs))
	}
}

func TestWithCoatFile_InvalidCoat(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "bad.yaml")
	// Coat without a URI — should fail validation.
	content := `
coats:
  - name: "missing-uri"
    response:
      code: 200
      body: "oops"
`
	if err := os.WriteFile(coatFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(WithCoatFile(coatFile))
	if len(srv.loadErrs) == 0 {
		t.Fatal("expected validation errors for coat without URI")
	}
}
