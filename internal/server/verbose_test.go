package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

func TestServe_VerboseLogging(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "verbose-test",
				Request:  coat.Request{Method: "GET", URI: "/test"},
				Response: &coat.Response{Code: 200, Body: "ok"},
			},
		},
	}

	var logBuf bytes.Buffer
	srv := server.New(coats, server.Config{
		Verbose: true,
		Logger:  slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Make a matching request — exercises verbose logRequest with coat name.
	resp, err := httpClient.Get(srv.URL() + "/test")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	assertEqual(t, "status", 200, resp.StatusCode)

	// Make a non-matching request — exercises verbose logRequest without coat name.
	resp2, err := httpClient.Get(srv.URL() + "/not-found")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	assertEqual(t, "404 status", 404, resp2.StatusCode)

	// Assert that verbose logging captured expected fields.
	logOutput := logBuf.String()
	for _, want := range []string{"matched=true", "coat=verbose-test", "matched=false", "status=404", "duration="} {
		if !strings.Contains(logOutput, want) {
			t.Errorf("expected log output to contain %q, got:\n%s", want, logOutput)
		}
	}
}

func TestServe_VerboseLogging_QualifiersAndFile(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(coatFile, []byte(`coats:
  - name: auth-search
    request:
      method: GET
      uri: /search
      headers:
        Authorization: "Bearer *"
      query:
        q: "*"
    response:
      code: 200
      body: "results"
`), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, errs := coat.LoadPaths([]string{coatFile})
	if len(errs) > 0 {
		t.Fatalf("load errors: %v", errs)
	}

	var logBuf bytes.Buffer
	srv := server.New(loaded, server.Config{
		Verbose: true,
		Logger:  slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	req, err := http.NewRequest("GET", srv.URL()+"/search?q=hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer token123")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	assertEqual(t, "status", 200, resp.StatusCode)

	logOutput := logBuf.String()
	// Should log the file path.
	if !strings.Contains(logOutput, "file=") {
		t.Errorf("expected 'file=' in log output, got:\n%s", logOutput)
	}
	// Should log qualifiers.
	if !strings.Contains(logOutput, "qualifiers=") {
		t.Errorf("expected 'qualifiers=' in log output, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "headers(1)") {
		t.Errorf("expected 'headers(1)' in qualifiers, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "query") {
		t.Errorf("expected 'query' in qualifiers, got:\n%s", logOutput)
	}
}

func TestServe_EmptyBody(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "no-body",
				Request:  coat.Request{Method: "DELETE", URI: "/resource"},
				Response: &coat.Response{Code: 204},
			},
		},
	}

	srv := startServer(t, coats)

	req, err := http.NewRequest("DELETE", srv.URL()+"/resource", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 204, resp.StatusCode)
	body := readBody(t, resp)
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
}

func TestServe_Addr_BeforeStart(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "unused",
				Request:  coat.Request{Method: "GET", URI: "/test"},
				Response: &coat.Response{Code: 200},
			},
		},
	}
	srv := server.New(coats, server.Config{})
	if srv.Addr() != "" {
		t.Fatalf("expected empty addr before Start(), got %q", srv.Addr())
	}
}

func TestVerbose404_IncludesNearMisses(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name: "auth-endpoint",
				Request: coat.Request{
					Method:  "GET",
					URI:     "/api/v1/users",
					Headers: map[string]string{"Authorization": "Bearer *"},
				},
				Response: &coat.Response{Code: 200, Body: "ok"},
			},
		},
	}

	var logBuf bytes.Buffer
	srv := server.New(coats, server.Config{
		Verbose: true,
		Logger:  slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Request without Authorization header should get a diagnostic 404.
	resp, err := httpClient.Get(srv.URL() + "/api/v1/users")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 404, resp.StatusCode)

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}

	rawNearMisses, ok := body["near_misses"]
	if !ok {
		t.Fatal("expected 'near_misses' key in 404 JSON response")
	}

	var nearMisses []map[string]string
	if err := json.Unmarshal(rawNearMisses, &nearMisses); err != nil {
		t.Fatalf("failed to unmarshal near_misses: %v", err)
	}
	if len(nearMisses) == 0 {
		t.Fatal("expected at least one near-miss")
	}
	if nearMisses[0]["coat_name"] != "auth-endpoint" {
		t.Errorf("expected coat_name 'auth-endpoint', got %q", nearMisses[0]["coat_name"])
	}
	if !strings.Contains(nearMisses[0]["reason"], "header mismatch") {
		t.Errorf("expected header mismatch reason, got %q", nearMisses[0]["reason"])
	}

	// Verify logging.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "near miss") {
		t.Errorf("expected 'near miss' in log output, got:\n%s", logOutput)
	}
}

func TestNonVerbose404_NoNearMisses(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "endpoint",
				Request:  coat.Request{Method: "POST", URI: "/api/v1/users"},
				Response: &coat.Response{Code: 201},
			},
		},
	}
	srv := startServer(t, coats)

	resp, err := httpClient.Get(srv.URL() + "/api/v1/users")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 404, resp.StatusCode)

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}

	if _, ok := body["near_misses"]; ok {
		t.Fatal("non-verbose mode should not include near_misses in 404 response")
	}
}

func TestServe_BodyFile_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	absFile := filepath.Join(dir, "absolute.json")
	if err := os.WriteFile(absFile, []byte(`{"abs": true}`), 0644); err != nil {
		t.Fatal(err)
	}

	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "abs-body",
				Request:  coat.Request{Method: "GET", URI: "/abs"},
				Response: &coat.Response{Code: 200, BodyFile: absFile},
			},
		},
	}
	srv := startServer(t, coats)

	resp, err := httpClient.Get(srv.URL() + "/abs")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"abs": true}`, readBody(t, resp))
}
