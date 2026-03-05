package proxy_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/proxy"
)

// httpClient is a shared test client with an explicit timeout to prevent
// tests from hanging indefinitely if the proxy or upstream stalls.
var httpClient = &http.Client{Timeout: 5 * time.Second}

func TestProxy_ForwardsRequest(t *testing.T) {
	// Set up a test upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"from": "upstream"}`))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{"Authorization", "Cookie", "Set-Cookie"},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Make a request through the proxy.
	resp, err := httpClient.Get(p.URL() + "/api/v1/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != `{"from": "upstream"}` {
		t.Fatalf("expected upstream body, got %s", body)
	}

	// Wait briefly for async coat file write.
	p.WaitCaptures()

	// Check that a coat file was captured.
	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one captured coat file")
	}

	// Read the captured file and verify basic structure.
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read captured coat file: %v", err)
	}
	if !strings.Contains(string(content), "/api/v1/users") {
		t.Fatalf("expected captured coat to contain URI, got: %s", content)
	}
}

func TestProxy_StripHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Authorization", "secret")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{"Authorization"},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	req, err := http.NewRequest("GET", p.URL()+"/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected captured coat file")
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read captured coat file: %v", err)
	}
	contentStr := string(content)
	if strings.Contains(contentStr, "secret-token") {
		t.Fatal("expected Authorization header to be stripped from captured coat")
	}
	if strings.Contains(contentStr, "secret") && strings.Contains(contentStr, "Authorization") {
		t.Fatal("expected Authorization response header to be stripped")
	}
}

func TestProxy_Filter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		Filter:       "/api/*",
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Request matching the filter.
	resp, err := httpClient.Get(p.URL() + "/api/users")
	if err != nil {
		t.Fatalf("filter-matched request failed: %v", err)
	}
	_ = resp.Body.Close()

	// Request NOT matching the filter.
	resp2, err := httpClient.Get(p.URL() + "/health")
	if err != nil {
		t.Fatalf("filter-excluded request failed: %v", err)
	}
	_ = resp2.Body.Close()

	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly 1 captured coat file (filter should exclude /health), got %d", len(files))
	}
}

func TestProxy_Dedupe_Skip(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "skip",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Make the same request twice.
	resp, err := httpClient.Get(p.URL() + "/test")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	resp2, err := httpClient.Get(p.URL() + "/test")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	_ = resp2.Body.Close()
	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly 1 file with skip dedup, got %d", len(files))
	}
}

func TestProxy_Dedupe_Append(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "append",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Make the same request three times.
	for i := 0; i < 3; i++ {
		resp, err := httpClient.Get(p.URL() + "/test")
		if err != nil {
			t.Fatalf("request %d failed: %v", i+1, err)
		}
		_ = resp.Body.Close()
		p.WaitCaptures()
	}

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files with append dedup, got %d", len(files))
	}
}

func TestProxy_FileNaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("created"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Post(p.URL()+"/api/v1/users", "application/json", strings.NewReader(`{"name": "test"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	expectedFile := filepath.Join(writeDir, "POST_api_v1_users_201.yaml")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		allFiles, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
		t.Fatalf("expected file POST_api_v1_users_201.yaml, found: %v", allFiles)
	}
}

func TestProxy_WaitCaptures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Make a request and use WaitCaptures instead of time.Sleep.
	resp, err := httpClient.Get(p.URL() + "/api/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	p.WaitCaptures()

	// File should exist immediately after WaitCaptures returns.
	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected captured coat file after WaitCaptures()")
	}
}

func TestProxy_CompressedUpstream(t *testing.T) {
	// Upstream that returns gzip-compressed content when Accept-Encoding: gzip is present.
	const plainBody = `{"message": "hello from compressed upstream"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			_, _ = gz.Write([]byte(plainBody))
			_ = gz.Close()
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write(buf.Bytes())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(plainBody))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Send a request with explicit Accept-Encoding: gzip through the proxy.
	req, err := http.NewRequest("GET", p.URL()+"/api/compressed", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	// Use a transport with DisableCompression so the client does NOT auto-decompress.
	client := &http.Client{
		Transport: &http.Transport{DisableCompression: true},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	// The proxy should relay the compressed response transparently.
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip in relayed response, got %q", resp.Header.Get("Content-Encoding"))
	}

	// Verify the relayed body is actually gzip-compressed (not plain text).
	gr, err := gzip.NewReader(bytes.NewReader(respBody))
	if err != nil {
		t.Fatalf("relayed body is not valid gzip: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to decompress relayed body: %v", err)
	}
	if err := gr.Close(); err != nil {
		t.Fatalf("failed to close gzip reader: %v", err)
	}
	if string(decompressed) != plainBody {
		t.Fatalf("decompressed relayed body = %q, want %q", decompressed, plainBody)
	}

	// Wait for the capture to be written.
	p.WaitCaptures()

	// Read the captured coat file and verify the body is decompressed (human-readable).
	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob captured coat files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one captured coat file")
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read captured coat file: %v", err)
	}
	contentStr := string(content)

	// The captured coat body must contain the plain text JSON, not gzip binary.
	if !strings.Contains(contentStr, "hello from compressed upstream") {
		t.Fatalf("expected coat file to contain decompressed body, got:\n%s", contentStr)
	}

	// Content-Encoding should NOT appear in the captured coat response headers.
	if strings.Contains(contentStr, "Content-Encoding") {
		t.Fatalf("expected coat file to NOT contain Content-Encoding header, got:\n%s", contentStr)
	}
}

func TestProxy_CaptureBody_Default(t *testing.T) {
	// By default, CaptureBody should be true and POST request bodies should be captured.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"received": "` + string(body) + `"}`))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Post(p.URL()+"/api/v1/users", "application/json", strings.NewReader(`{"name": "alice"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected captured coat file")
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read captured file: %v", err)
	}

	// Parse the captured coat and assert on the request body structurally.
	var captured coat.File
	if err := yaml.Unmarshal(content, &captured); err != nil {
		t.Fatalf("failed to unmarshal captured coat: %v", err)
	}
	if len(captured.Coats) == 0 {
		t.Fatal("expected at least one coat in captured file")
	}
	wantBody := `{"name": "alice"}`
	if captured.Coats[0].Request.Body == nil || *captured.Coats[0].Request.Body != wantBody {
		var got string
		if captured.Coats[0].Request.Body != nil {
			got = *captured.Coats[0].Request.Body
		}
		t.Fatalf("expected request body %q, got %q", wantBody, got)
	}
}

func TestProxy_CaptureBody_Disabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		CaptureBody:  boolPtr(false),
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Post(p.URL()+"/api/v1/users", "application/json", strings.NewReader(`{"name": "bob"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected captured coat file")
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read captured file: %v", err)
	}

	// Parse the captured coat and assert the request body is absent.
	var captured coat.File
	if err := yaml.Unmarshal(content, &captured); err != nil {
		t.Fatalf("failed to unmarshal captured coat: %v", err)
	}
	if len(captured.Coats) == 0 {
		t.Fatal("expected at least one coat in captured file")
	}
	if captured.Coats[0].Request.Body != nil {
		t.Fatalf("expected nil request body when CaptureBody is disabled, got %q", *captured.Coats[0].Request.Body)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestSanitisePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/api/v1/users", "api_v1_users"},
		{"/api/v1/users/123", "api_v1_users_123"},
		{"/", "root"},
		{"/special!chars@here", "specialcharshere"},
	}

	for _, tt := range tests {
		got := proxy.SanitisePath(tt.input)
		if got != tt.expected {
			t.Errorf("sanitisePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
