package proxy_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/proxy"
	"github.com/yesdevnull/trenchcoat/internal/server"
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

func TestProxy_InvalidGzipBody(t *testing.T) {
	// Upstream claims gzip encoding but body is not valid gzip data.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("this is not gzip data"))
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

	// Use a client with DisableCompression so it doesn't auto-decompress.
	client := &http.Client{
		Transport: &http.Transport{DisableCompression: true},
		Timeout:   5 * time.Second,
	}
	req, err := http.NewRequest("GET", p.URL()+"/api/bad-gzip", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	p.WaitCaptures()

	// Coat file should still be written — with the raw (non-gzip) body as fallback.
	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected coat file to be written even with invalid gzip")
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	// The raw body "this is not gzip data" should appear since decompression failed.
	if !strings.Contains(string(content), "this is not gzip data") {
		t.Fatalf("expected raw body in coat file, got:\n%s", content)
	}
}

func TestProxy_Filter_InvalidPattern(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		Filter:       "[invalid-pattern", // Malformed glob — unclosed bracket.
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

	// Request should still succeed (proxied to upstream).
	resp, err := httpClient.Get(p.URL() + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	p.WaitCaptures()

	// No coat file should be captured — shouldCapture returns false on error.
	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no captured files with invalid filter, got %d", len(files))
	}
}

func TestSingleJoiningSlash(t *testing.T) {
	// Exercise the branches of singleJoiningSlash by proxying through
	// upstreams with different base path configurations. HTTP request
	// paths always start with "/", so the only reachable branches are:
	//   - both_slashes: upstream trailing "/" + request leading "/" → trim one
	//   - default:      upstream no trailing "/" + request leading "/" → concatenate
	tests := []struct {
		name         string
		upstreamPath string // Upstream base path (may have trailing slash).
		requestPath  string // Client request path (always has leading slash).
		wantContains string // Expected path fragment upstream receives.
	}{
		{
			name:         "both_slashes",
			upstreamPath: "/base/",
			requestPath:  "/endpoint",
			wantContains: "/base/endpoint",
		},
		{
			name:         "no_trailing_slash",
			upstreamPath: "/base",
			requestPath:  "/endpoint",
			wantContains: "/base/endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pathCh := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pathCh <- r.URL.Path
				w.WriteHeader(200)
				_, _ = w.Write([]byte("ok"))
			}))
			defer upstream.Close()

			p, err := proxy.New(proxy.Config{
				UpstreamURL:  upstream.URL + tt.upstreamPath,
				WriteDir:     t.TempDir(),
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

			resp, err := httpClient.Get(p.URL() + tt.requestPath)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			_ = resp.Body.Close()

			var receivedPath string
			select {
			case receivedPath = <-pathCh:
			case <-time.After(5 * time.Second):
				t.Fatalf("timed out waiting for upstream request")
			}
			if !strings.Contains(receivedPath, tt.wantContains) {
				t.Fatalf("expected upstream path to contain %q, got %q", tt.wantContains, receivedPath)
			}
		})
	}
}

func TestProxy_RedirectHandling(t *testing.T) {
	// Upstream returns a 301 redirect. The proxy should capture and relay
	// the 3xx response as-is, not follow the redirect.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			w.Header().Set("Location", "/new")
			w.WriteHeader(301)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("new page"))
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

	// Client that does NOT follow redirects.
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}

	resp, err := noRedirectClient.Get(p.URL() + "/old")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != 301 {
		t.Fatalf("expected 301, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/new" {
		t.Fatalf("expected Location: /new, got %q", resp.Header.Get("Location"))
	}

	p.WaitCaptures()

	// Verify the 301 was captured.
	files, err := filepath.Glob(filepath.Join(writeDir, "*301*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected captured coat file for 301 redirect")
	}
}

func TestProxy_NoHeaders(t *testing.T) {
	// When NoHeaders is true, captured coat files must not contain ANY headers
	// in either the request or response sections.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "abc123")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    writeDir,
		NoHeaders:   true,
		Dedupe:      "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	req, err := http.NewRequest("GET", p.URL()+"/api/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
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

	// Parse the captured coat and verify no headers are present.
	var captured coat.File
	if err := yaml.Unmarshal(content, &captured); err != nil {
		t.Fatalf("failed to unmarshal captured coat: %v", err)
	}
	if len(captured.Coats) == 0 {
		t.Fatal("expected at least one coat")
	}
	c := captured.Coats[0]

	// Neither request nor response headers should be in the coat file.
	contentStr := string(content)
	if strings.Contains(contentStr, "headers:") {
		t.Fatalf("expected no headers in captured coat with NoHeaders=true, got:\n%s", contentStr)
	}

	// The response body should still be captured.
	if c.Response.Body != `{"ok": true}` {
		t.Fatalf("expected response body to be captured, got %q", c.Response.Body)
	}
}

func TestProxy_NoHeaders_StripHeaders_MutuallyExclusive(t *testing.T) {
	// NoHeaders and StripHeaders cannot both be set.
	_, err := proxy.New(proxy.Config{
		UpstreamURL:  "http://localhost:9999",
		WriteDir:     t.TempDir(),
		NoHeaders:    true,
		StripHeaders: []string{"Authorization"},
		Dedupe:       "overwrite",
	})
	if err == nil {
		t.Fatal("expected error when both NoHeaders and StripHeaders are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

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

func TestProxy_PrettyJSON(t *testing.T) {
	compactJSON := `{"users":[{"id":1,"name":"alice"},{"id":2,"name":"bob"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(compactJSON))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		PrettyJSON:   true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/users")
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
		t.Fatalf("failed to read: %v", err)
	}
	contentStr := string(content)

	// Verify it's valid JSON when extracted.
	var captured coat.File
	if err := yaml.Unmarshal(content, &captured); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	body := captured.Coats[0].Response.Body
	if !json.Valid([]byte(body)) {
		t.Fatalf("expected valid JSON in response body, got: %s", body)
	}

	// Pretty JSON should match json.Indent output (i.e., include indentation/newlines).
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(compactJSON), "", "  "); err != nil {
		t.Fatalf("failed to indent JSON: %v", err)
	}
	if body != pretty.String() {
		t.Fatalf("expected pretty-printed JSON body:\n%s\n\ngot:\n%s\n\nfull coat file:\n%s", pretty.String(), body, contentStr)
	}
}

func TestProxy_ConcurrentCapturesSameBaseName(t *testing.T) {
	// The query string is deliberately excluded from the generated base name, so
	// two concurrent requests differing only by query resolve to one filename
	// and race to write it. Whichever wins, the file must be a complete coat --
	// never one response's prefix followed by the other's tail.
	//
	// This holds today because os.WriteFile issues a single write syscall and
	// the kernel serialises writes to a regular file. It is an invariant worth
	// pinning: chunking the write, or switching to an io.Writer, would break it
	// silently and leave unparseable fixtures behind.
	long := strings.Repeat("A", 200*1024)
	short := strings.Repeat("B", 64)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := short
		if r.URL.Query().Get("page") == "1" {
			body = long
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
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

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(10 * time.Second) })

	for range 40 {
		var wg sync.WaitGroup
		for _, page := range []string{"1", "2"} {
			wg.Add(1)
			go func(page string) {
				defer wg.Done()
				resp, err := httpClient.Get(p.URL() + "/api/items?page=" + page)
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}(page)
		}
		wg.Wait()
		p.WaitCaptures()

		captured := readOnlyCoat(t, writeDir)
		body := captured.Coats[0].Response.Body
		if body != long && body != short {
			t.Fatalf("captured coat body is neither response intact: %d bytes, starts %q, ends %q",
				len(body), truncate(body, 16), truncate(body[max(0, len(body)-16):], 16))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func TestProxy_DedupeSkip_ConcurrentSameRequest(t *testing.T) {
	// 'skip' promises one file per distinct request, and must keep that promise
	// when the captures are concurrent.
	//
	// This reproduces the pre-fix bug intermittently, not reliably: roughly one
	// run in four under -race. Every capture asks whether a file already exists
	// and then writes, and if the check and the write are not one atomic step
	// they all see an empty directory. The generated name is stamped with the
	// current second, so they still converge on a single path unless the batch
	// happens to straddle a second boundary -- which is what makes it a coin
	// toss rather than a proof. Aligning the release to the boundary does not
	// help, because the delay between releasing a request and its capture
	// choosing a name varies by more than the window being aimed at.
	//
	// It is kept because it did catch the real thing (two files, timestamps one
	// second apart) and because the invariant is worth stating, but it is not
	// load-bearing: the lock is.
	const requests = 16

	// Hold every request in the upstream until all of them have arrived, so the
	// captures start together and genuinely overlap. Without this the first
	// capture finishes before the next request is even made, and the check and
	// the write never interleave. The body is large enough that marshalling and
	// writing it takes long enough to matter.
	release := make(chan struct{})
	var arrived sync.WaitGroup
	arrived.Add(requests)
	body := strings.Repeat("C", 512*1024)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived.Done()
		<-release
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
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

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(10 * time.Second) })

	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := httpClient.Get(p.URL() + "/api/items")
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}
	arrived.Wait()
	close(release)
	wg.Wait()
	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("dedupe=skip wrote %d files for the same request, want 1: %v", len(files), files)
	}

	captured := readOnlyCoat(t, writeDir)
	if got := captured.Coats[0].Response.Body; got != body {
		t.Fatalf("captured body is torn: got %d bytes, want %d", len(got), len(body))
	}
}

func TestProxy_PrettyJSON_DropsContentLength(t *testing.T) {
	// Pretty-printing changes the body length, so the upstream's Content-Length
	// no longer describes the captured body and must not be recorded.
	compactJSON := `{"users":[{"id":1,"name":"alice"},{"id":2,"name":"bob"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(compactJSON)))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(compactJSON))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		PrettyJSON:   true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	captured := readOnlyCoat(t, writeDir)
	for k, v := range captured.Coats[0].Response.Headers {
		if strings.EqualFold(k, "Content-Length") {
			t.Fatalf("captured coat kept Content-Length %q, which no longer matches the pretty-printed body of %d bytes",
				v, len(captured.Coats[0].Response.Body))
		}
	}
}

func TestProxy_PrettyJSON_CapturedCoatReplays(t *testing.T) {
	// The point of capturing is replay: serving the captured coat must return
	// the whole pretty-printed body, not a body truncated to a stale length.
	compactJSON := `{"users":[{"id":1,"name":"alice"},{"id":2,"name":"bob"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(compactJSON)))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(compactJSON))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		PrettyJSON:   true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("expected a captured coat file, glob err %v, files %v", err, files)
	}

	loaded, errs := coat.LoadPaths([]string{files[0]})
	if len(errs) > 0 {
		t.Fatalf("captured coat did not load: %v", errs)
	}

	mock := server.New(loaded, server.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	addr, err := mock.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock: %v", err)
	}
	t.Cleanup(func() { _ = mock.Shutdown(5 * time.Second) })

	replayed, err := httpClient.Get("http://" + addr + "/api/users")
	if err != nil {
		t.Fatalf("replay request failed: %v", err)
	}
	defer func() { _ = replayed.Body.Close() }()

	body, err := io.ReadAll(replayed.Body)
	if err != nil {
		t.Fatalf("reading replayed body failed: %v", err)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(compactJSON), "", "  "); err != nil {
		t.Fatalf("failed to indent JSON: %v", err)
	}
	if string(body) != pretty.String() {
		t.Fatalf("replaying the captured coat returned %d bytes, want the %d-byte pretty body\ngot:  %q\nwant: %q",
			len(body), pretty.Len(), string(body), pretty.String())
	}
}

// readOnlyCoat parses the single captured coat file in dir, failing if there is
// not exactly one.
func readOnlyCoat(t *testing.T, dir string) coat.File {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly one captured coat file, got %v", files)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read %s: %v", files[0], err)
	}

	var parsed coat.File
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", files[0], err)
	}
	return parsed
}

func TestProxy_PrettyJSON_NonJSON(t *testing.T) {
	// Non-JSON responses should not be affected by PrettyJSON.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("just plain text"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     writeDir,
		StripHeaders: []string{},
		Dedupe:       "overwrite",
		PrettyJSON:   true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/plain")
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
		t.Fatalf("failed to read: %v", err)
	}
	if !strings.Contains(string(content), "just plain text") {
		t.Fatalf("expected plain text body, got:\n%s", content)
	}
}

func TestProxy_BodyFileThreshold(t *testing.T) {
	largeBody := strings.Repeat("x", 200)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(largeBody))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:       upstream.URL,
		WriteDir:          writeDir,
		StripHeaders:      []string{},
		Dedupe:            "overwrite",
		BodyFileThreshold: 100,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/large")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	coatFiles, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(coatFiles) == 0 {
		t.Fatal("expected captured coat file")
	}

	content, err := os.ReadFile(coatFiles[0])
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	// The coat file should use body_file instead of inline body.
	var captured coat.File
	if err := yaml.Unmarshal(content, &captured); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if captured.Coats[0].Response.Body != "" {
		t.Fatal("expected body to be empty when body_file threshold exceeded")
	}
	if captured.Coats[0].Response.BodyFile == "" {
		t.Fatal("expected body_file to be set when threshold exceeded")
	}

	// Verify the body file exists and has the correct content.
	bodyFilePath := filepath.Join(writeDir, captured.Coats[0].Response.BodyFile)
	bodyContent, err := os.ReadFile(bodyFilePath)
	if err != nil {
		t.Fatalf("failed to read body file: %v", err)
	}
	if string(bodyContent) != largeBody {
		t.Fatalf("body file content mismatch: got %d bytes, want %d", len(bodyContent), len(largeBody))
	}
}

func TestProxy_BodyFileThreshold_SmallBody(t *testing.T) {
	// Bodies under the threshold should remain inline.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("small"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL:       upstream.URL,
		WriteDir:          writeDir,
		StripHeaders:      []string{},
		Dedupe:            "overwrite",
		BodyFileThreshold: 100,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/small")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	coatFiles, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatalf("failed to glob: %v", err)
	}
	if len(coatFiles) == 0 {
		t.Fatal("expected captured coat file")
	}

	content, err := os.ReadFile(coatFiles[0])
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	var captured coat.File
	if err := yaml.Unmarshal(content, &captured); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if captured.Coats[0].Response.Body != "small" {
		t.Fatalf("expected inline body 'small', got %q", captured.Coats[0].Response.Body)
	}
	if captured.Coats[0].Response.BodyFile != "" {
		t.Fatal("expected no body_file for small body")
	}
}

func TestProxy_NameTemplate(t *testing.T) {
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
		NameTemplate: "{{.Method}}-{{.Path}}-{{.Status}}",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Post(p.URL()+"/api/v1/users", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	expectedFile := filepath.Join(writeDir, "POST-api_v1_users-201.yaml")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		allFiles, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
		t.Fatalf("expected file POST-api_v1_users-201.yaml, found: %v", allFiles)
	}
}

func TestProxyCapturesConcurrencyBounded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    writeDir,
		Dedupe:      "append",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	addr, err := p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	const numRequests = 30
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			resp, err := client.Get(fmt.Sprintf("http://%s/path/%d", addr, n))
			if err != nil {
				t.Errorf("request %d failed: %v", n, err)
				return
			}
			_ = resp.Body.Close()
		}(i)
	}
	wg.Wait()

	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected captured coat files, got none")
	}
	if len(files) < 20 {
		t.Errorf("expected at least 20 coat files, got %d", len(files))
	}

	_ = p.Shutdown(5 * time.Second)
}

func TestProxyShutdownRespectsTimeoutWithPendingCaptures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	writeDir := t.TempDir()

	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    writeDir,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	addr, err := p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + addr + "/test")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	done := make(chan error, 1)
	go func() {
		done <- p.Shutdown(500 * time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not return within timeout")
	}
}

func TestProxy_PreservesPercentEncodedPath(t *testing.T) {
	// The proxy rebuilt the upstream URL from the decoded r.URL.Path, so an
	// encoded separator was handed to the upstream as a real one and reached a
	// different route than the client asked for. A capture tool that quietly
	// rewrites the request is recording something that never happened.
	seen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.RequestURI
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstream.URL,
		WriteDir:     t.TempDir(),
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/seg%2Fment/tail")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	p.WaitCaptures()

	got := <-seen
	if got != "/seg%2Fment/tail" {
		t.Fatalf("upstream saw %q, want %q -- the encoded separator was decoded in transit", got, "/seg%2Fment/tail")
	}
}
