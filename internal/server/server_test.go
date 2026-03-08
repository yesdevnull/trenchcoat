package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

// httpClient is a shared test client with an explicit timeout to prevent
// tests from hanging indefinitely if the server stalls.
var httpClient = &http.Client{Timeout: 5 * time.Second}

func TestServe_BasicResponse(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "hello",
				Request:  coat.Request{Method: "GET", URI: "/hello"},
				Response: &coat.Response{Code: 200, Body: "world"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/hello")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
	body := readBody(t, resp)
	assertEqual(t, "body", "world", body)
}

func TestServe_ResponseHeaders(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:    "json",
				Request: coat.Request{Method: "GET", URI: "/json"},
				Response: &coat.Response{
					Code:    200,
					Headers: map[string]string{"Content-Type": "application/json", "X-Custom": "test"},
					Body:    `{"ok": true}`,
				},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/json")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "content-type", "application/json", resp.Header.Get("Content-Type"))
	assertEqual(t, "x-custom", "test", resp.Header.Get("X-Custom"))
}

func TestServe_404_NoMatch(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "hello",
				Request:  coat.Request{Method: "GET", URI: "/hello"},
				Response: &coat.Response{Code: 200},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/missing")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 404, resp.StatusCode)

	var errBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	assertEqual(t, "error", "no matching coat", errBody["error"])
	assertEqual(t, "method", "GET", errBody["method"])
	assertEqual(t, "uri", "/missing", errBody["uri"])
}

func TestServe_DefaultStatusCode(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "default-code",
				Request:  coat.Request{Method: "GET", URI: "/test"},
				Response: &coat.Response{Body: "ok"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
}

func TestServe_BodyFile(t *testing.T) {
	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixtureDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "data.json"), []byte(`{"from": "file"}`), 0644); err != nil {
		t.Fatal(err)
	}

	coatFilePath := filepath.Join(dir, "coat.yaml")

	srv := startServer(t, []coat.LoadedCoat{
		{
			FilePath: coatFilePath,
			Coat: coat.Coat{
				Name:     "body-file",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, BodyFile: "fixtures/data.json"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/data")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
	body := readBody(t, resp)
	assertEqual(t, "body", `{"from": "file"}`, body)
}

func TestServe_BodyFile_Missing(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			FilePath: "/tmp/nonexistent/coat.yaml",
			Coat: coat.Coat{
				Name:     "missing-file",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, BodyFile: "does-not-exist.json"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/data")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 500, resp.StatusCode)

	var errBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	assertEqual(t, "error", "body_file not found", errBody["error"])
}

func TestServe_DelayMs(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "delayed",
				Request:  coat.Request{Method: "GET", URI: "/slow"},
				Response: &coat.Response{Code: 200, Body: "ok", DelayMs: 100},
			},
		},
	})

	start := time.Now()
	resp, err := httpClient.Get(srv.URL() + "/slow")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if elapsed < 90*time.Millisecond {
		t.Fatalf("expected at least 90ms delay, got %v", elapsed)
	}
	assertEqual(t, "status", 200, resp.StatusCode)
}

func TestServe_DelayJitter(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "jittery",
				Request:  coat.Request{Method: "GET", URI: "/jitter"},
				Response: &coat.Response{Code: 200, Body: "ok", DelayMs: 50, DelayJitterMs: 50},
			},
		},
	})

	// Make several requests — all should take at least 50ms (the base delay).
	for range 3 {
		start := time.Now()
		resp, err := httpClient.Get(srv.URL() + "/jitter")
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		_ = resp.Body.Close()
		assertEqual(t, "status", 200, resp.StatusCode)
		if elapsed < 50*time.Millisecond {
			t.Fatalf("expected at least 50ms delay, got %v", elapsed)
		}
	}
}

func TestServe_DelayJitter_OnlyJitter(t *testing.T) {
	// Jitter without base delay should still work.
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "jitter-only",
				Request:  coat.Request{Method: "GET", URI: "/jitter-only"},
				Response: &coat.Response{Code: 200, Body: "ok", DelayJitterMs: 10},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/jitter-only")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	assertEqual(t, "status", 200, resp.StatusCode)
}

func TestServe_GlobMatching(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "glob",
				Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
				Response: &coat.Response{Code: 200, Body: "user"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/api/v1/users/42")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", "user", readBody(t, resp))
}

func TestServe_RegexMatching(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "regex",
				Request:  coat.Request{Method: "GET", URI: `~/api/v1/users/\d+`},
				Response: &coat.Response{Code: 200, Body: "numeric-user"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/api/v1/users/123")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", "numeric-user", readBody(t, resp))

	resp2, err := httpClient.Get(srv.URL() + "/api/v1/users/abc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	assertEqual(t, "status", 404, resp2.StatusCode)
}

func TestServe_MethodDifferentiation(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "get-users",
				Request:  coat.Request{Method: "GET", URI: "/api/users"},
				Response: &coat.Response{Code: 200, Body: "list"},
			},
		},
		{
			Coat: coat.Coat{
				Name:     "post-users",
				Request:  coat.Request{Method: "POST", URI: "/api/users"},
				Response: &coat.Response{Code: 201, Body: "created"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/api/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "GET status", 200, resp.StatusCode)
	assertEqual(t, "GET body", "list", readBody(t, resp))

	resp2, err := httpClient.Post(srv.URL()+"/api/users", "", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "POST status", 201, resp2.StatusCode)
	assertEqual(t, "POST body", "created", readBody(t, resp2))
}

func TestServe_HeaderMatching(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name: "with-auth",
				Request: coat.Request{
					Method:  "GET",
					URI:     "/protected",
					Headers: map[string]string{"Authorization": "Bearer *"},
				},
				Response: &coat.Response{Code: 200, Body: "authorised"},
			},
		},
	})

	// With header — should match.
	req, err := http.NewRequest("GET", srv.URL()+"/protected", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer my-token")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)

	// Without header — no match.
	resp2, err := httpClient.Get(srv.URL() + "/protected")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "status", 404, resp2.StatusCode)
}

// --- Double-slash (protocol-in-path) tests ---

func TestServe_DoubleSlashInPath(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "protocol-in-path",
				Request:  coat.Request{Method: "GET", URI: "/Path/To/Json/swis://Hostname/Another/Thing"},
				Response: &coat.Response{Code: 200, Body: "matched"},
			},
		},
	})

	// Build request manually to preserve the double slash.
	req, err := http.NewRequest("GET", srv.URL()+"/Path/To/Json/swis://Hostname/Another/Thing", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	// Override the parsed URL to preserve the double slash in the path.
	req.URL.Opaque = "/Path/To/Json/swis://Hostname/Another/Thing"

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", "matched", readBody(t, resp))
}

func TestServe_DoubleSlashInPath_404Response(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "protocol-in-path",
				Request:  coat.Request{Method: "GET", URI: "/Path/To/Json/swis://Hostname/Another/Thing"},
				Response: &coat.Response{Code: 200, Body: "matched"},
			},
		},
	})

	// Request with single slash — should NOT match coat with double slash.
	resp, err := httpClient.Get(srv.URL() + "/Path/To/Json/swis:/Hostname/Another/Thing")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 404, resp.StatusCode)
}

// --- Sequence tests ---

func TestServe_Sequence_Cycle(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:    "cycle-seq",
				Request: coat.Request{Method: "GET", URI: "/health"},
				Responses: []coat.Response{
					{Code: 503, Body: "down"},
					{Code: 200, Body: "up"},
				},
				Sequence: "cycle",
			},
		},
	})

	// First request: 503.
	resp, err := httpClient.Get(srv.URL() + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "first status", 503, resp.StatusCode)
	assertEqual(t, "first body", "down", readBody(t, resp))

	// Second request: 200.
	resp2, err := httpClient.Get(srv.URL() + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "second status", 200, resp2.StatusCode)
	assertEqual(t, "second body", "up", readBody(t, resp2))

	// Third request: cycles back to 503.
	resp3, err := httpClient.Get(srv.URL() + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	assertEqual(t, "third status", 503, resp3.StatusCode)
}

func TestServe_Sequence_Once(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:    "once-seq",
				Request: coat.Request{Method: "GET", URI: "/once"},
				Responses: []coat.Response{
					{Code: 200, Body: "first"},
					{Code: 200, Body: "second"},
				},
				Sequence: "once",
			},
		},
	})

	// First request.
	resp, err := httpClient.Get(srv.URL() + "/once")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "first body", "first", readBody(t, resp))

	// Second request.
	resp2, err := httpClient.Get(srv.URL() + "/once")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "second body", "second", readBody(t, resp2))

	// Third request: exhausted, 404.
	resp3, err := httpClient.Get(srv.URL() + "/once")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	assertEqual(t, "exhausted status", 404, resp3.StatusCode)

	var errBody map[string]string
	if err := json.NewDecoder(resp3.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	assertEqual(t, "error", "sequence exhausted", errBody["error"])
	assertEqual(t, "coat", "once-seq", errBody["coat"])
}

func TestServe_Sequence_DefaultCycle(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:    "default-cycle",
				Request: coat.Request{Method: "GET", URI: "/dc"},
				Responses: []coat.Response{
					{Code: 200, Body: "a"},
					{Code: 200, Body: "b"},
				},
				// No explicit sequence — defaults to cycle.
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/dc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "first", "a", readBody(t, resp))

	resp2, err := httpClient.Get(srv.URL() + "/dc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "second", "b", readBody(t, resp2))

	resp3, err := httpClient.Get(srv.URL() + "/dc")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()
	assertEqual(t, "third (cycle)", "a", readBody(t, resp3))
}

// --- resolveBodyFile ambiguity tests ---

func TestServe_BodyFile_AmbiguousCoatSources(t *testing.T) {
	// Two coat files define a coat with the same name/URI/method but different
	// file paths. resolveBodyFile should detect the ambiguity and return 500.
	dirA := t.TempDir()
	dirB := t.TempDir()

	// Create body files in both directories.
	if err := os.WriteFile(filepath.Join(dirA, "data.json"), []byte(`{"from": "A"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "data.json"), []byte(`{"from": "B"}`), 0644); err != nil {
		t.Fatal(err)
	}

	srv := startServer(t, []coat.LoadedCoat{
		{
			FilePath: filepath.Join(dirA, "coat.yaml"),
			Coat: coat.Coat{
				Name:     "ambiguous",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, BodyFile: "data.json"},
			},
		},
		{
			FilePath: filepath.Join(dirB, "coat.yaml"),
			Coat: coat.Coat{
				Name:     "ambiguous",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, BodyFile: "data.json"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/data")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 500, resp.StatusCode)

	var errBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	assertEqual(t, "error", "body_file not found", errBody["error"])
}

func TestServe_BodyFile_SameCoatFilePath_NoAmbiguity(t *testing.T) {
	// Two coats from the same file path — no ambiguity, resolves correctly.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"ok": true}`), 0644); err != nil {
		t.Fatal(err)
	}
	coatFilePath := filepath.Join(dir, "coat.yaml")

	srv := startServer(t, []coat.LoadedCoat{
		{
			FilePath: coatFilePath,
			Coat: coat.Coat{
				Name:     "same-source",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, BodyFile: "data.json"},
			},
		},
		{
			FilePath: coatFilePath,
			Coat: coat.Coat{
				Name:     "same-source",
				Request:  coat.Request{Method: "GET", URI: "/data"},
				Response: &coat.Response{Code: 200, BodyFile: "data.json"},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/data")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"ok": true}`, readBody(t, resp))
}

// --- Response templating ---

func TestServe_ResponseTemplate_Method(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "echo-method",
				Request:  coat.Request{Method: "ANY", URI: "/echo"},
				Response: &coat.Response{Code: 200, Body: `{"method": "{{.Method}}"}`},
			},
		},
	})

	resp, err := httpClient.Post(srv.URL()+"/echo", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"method": "POST"}`, readBody(t, resp))
}

func TestServe_ResponseTemplate_Path(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "echo-path",
				Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
				Response: &coat.Response{Code: 200, Body: `{"path": "{{.Path}}"}`},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/api/v1/users/123")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"path": "/api/v1/users/123"}`, readBody(t, resp))
}

func TestServe_ResponseTemplate_PathSegment(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "echo-segment",
				Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
				Response: &coat.Response{Code: 200, Body: `{"id": "{{.Segment 3}}"}`},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/api/v1/users/456")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"id": "456"}`, readBody(t, resp))
}

func TestServe_ResponseTemplate_QueryParam(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "echo-query",
				Request:  coat.Request{Method: "GET", URI: "/search"},
				Response: &coat.Response{Code: 200, Body: `{"q": "{{.Query "q"}}"}`},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/search?q=hello")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"q": "hello"}`, readBody(t, resp))
}

func TestServe_ResponseTemplate_Body(t *testing.T) {
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "echo-body",
				Request:  coat.Request{Method: "POST", URI: "/echo"},
				Response: &coat.Response{Code: 200, Body: `{"echoed": "{{.Body}}"}`},
			},
		},
	})

	resp, err := httpClient.Post(srv.URL()+"/echo", "text/plain", strings.NewReader("ping"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)
	assertEqual(t, "body", `{"echoed": "ping"}`, readBody(t, resp))
}

func TestServe_ResponseTemplate_NoTemplate(t *testing.T) {
	// Bodies without {{ should be returned as-is.
	srv := startServer(t, []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "plain",
				Request:  coat.Request{Method: "GET", URI: "/plain"},
				Response: &coat.Response{Code: 200, Body: `no templates here`},
			},
		},
	})

	resp, err := httpClient.Get(srv.URL() + "/plain")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "body", "no templates here", readBody(t, resp))
}

// --- Helpers ---

func startServer(t *testing.T, coats []coat.LoadedCoat) *server.Server {
	t.Helper()
	srv := server.New(coats, server.Config{})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Shutdown(5 * time.Second)
	})
	return srv
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	return string(b)
}

func assertEqual[T comparable](t *testing.T, field string, expected, actual T) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", field, expected, actual)
	}
}

func TestCalls_ReturnsClonedHeaders(t *testing.T) {
	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "clone-test",
				Request:  coat.Request{Method: "GET", URI: "/clone"},
				Response: &coat.Response{Code: 200, Body: "ok"},
			},
		},
	}

	srv := server.New(coats, server.Config{RecordCalls: true})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	req, _ := http.NewRequest("GET", srv.URL()+"/clone", nil)
	req.Header.Set("X-Test", "original")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	// Get captured requests and mutate the returned header.
	calls := srv.Calls("clone-test")
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	calls[0].Header.Set("X-Test", "mutated")

	// Fetch again — the internal storage should be unaffected.
	calls2 := srv.Calls("clone-test")
	if calls2[0].Header.Get("X-Test") != "original" {
		t.Errorf("expected internal header to remain 'original', got %q", calls2[0].Header.Get("X-Test"))
	}
}
