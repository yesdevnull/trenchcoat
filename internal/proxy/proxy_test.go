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
	// A filter that cannot compile used to be accepted, then rejected every
	// request inside shouldCapture: one error logged per request, nothing
	// captured, and --verbose reporting "captured=false" exactly as it would
	// for a deliberate filter miss. Rejecting it at construction says so once,
	// before anything is proxied.
	_, err := proxy.New(proxy.Config{
		UpstreamURL:  "http://127.0.0.1:1",
		WriteDir:     t.TempDir(),
		Filter:       "[invalid-pattern", // Malformed glob — unclosed bracket.
		StripHeaders: []string{},
		Dedupe:       "overwrite",
	})
	if err == nil {
		t.Fatal("expected proxy.New to reject an uncompilable --filter pattern")
	}
	if !strings.Contains(err.Error(), "[invalid-pattern") {
		t.Fatalf("error should name the offending pattern, got: %v", err)
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
	// Two things combine to give that: writes go to a temp file and are renamed
	// into place, which is atomic, and captures resolving to the same name are
	// serialised so they do not share that temp file.
	//
	// Both responses are the same size and both requests are released together,
	// so the two writes actually overlap. An earlier version of this test used
	// 200 KiB against 64 B, which kept the captures tens of milliseconds apart
	// and hid the race completely.
	long := strings.Repeat("A", 200*1024)
	short := strings.Repeat("B", 200*1024)

	release := make(chan struct{})
	var arrived sync.WaitGroup

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := short
		if r.URL.Query().Get("page") == "1" {
			body = long
		}
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
		release = make(chan struct{})
		arrived.Add(2)

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

		// Both handlers are parked in the upstream; release them together so the
		// two captures reach their writes at the same moment.
		arrived.Wait()
		close(release)

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

// TestProxy_EveryRequestProducesOneCapture checks capture completeness under
// concurrency: 30 distinct paths must yield 30 coat files. It does not test
// captureSem -- deleting the semaphore leaves it green, because nothing here
// observes how many captures run at once. TestProxy_CaptureConcurrencyIsBounded
// covers that.
func TestProxy_EveryRequestProducesOneCapture(t *testing.T) {
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
	// Exactly one file per request: a lower-bound assertion would tolerate
	// captures silently lost to a filename collision or a swallowed write error.
	if len(files) != numRequests {
		t.Errorf("expected %d coat files, one per request, got %d", numRequests, len(files))
	}

	_ = p.Shutdown(5 * time.Second)
}

func TestProxy_PreservesPercentEncodedPath(t *testing.T) {
	// An encoded separator must reach the upstream as the client wrote it. A
	// capture tool that rewrites the request in transit records something that
	// never happened.
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

func TestProxy_CaptureOmitsClientSpecificHeaders(t *testing.T) {
	// Captured request headers become mandatory match constraints at replay, so
	// anything the client happened to send -- its User-Agent, its content
	// negotiation, the transport's Accept-Encoding -- silently ties the coat to
	// the tool that recorded it. Headers that genuinely qualify the request must
	// still be kept.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":1}`))
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
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	req, err := http.NewRequest("POST", p.URL()+"/api/users", strings.NewReader(`{"n":1}`))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("User-Agent", "curl/8.7.1")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-AU")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "abc123")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	captured := readOnlyCoat(t, writeDir)
	headers := captured.Coats[0].Request.Headers

	// Host and Connection are deliberately absent from this list. net/http moves
	// the host to r.Host and deletes it from r.Header, and this client sends no
	// Connection header, so neither could fail here regardless of what the
	// capture path does. Connection-scoped headers are covered by
	// TestProxy_CaptureOmitsConnectionScopedHeaders.
	for _, unwanted := range []string{"User-Agent", "Accept", "Accept-Encoding", "Accept-Language", "Content-Length"} {
		for k := range headers {
			if strings.EqualFold(k, unwanted) {
				t.Errorf("captured coat records %s: %q, which ties the coat to the client that recorded it", k, headers[k])
			}
		}
	}

	if got := headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type qualifies the request and must be kept, got %q", got)
	}
	if got := headers["X-Api-Key"]; got != "abc123" {
		t.Errorf("custom headers qualify the request and must be kept, got %q", got)
	}
}

func TestProxy_CapturedCoatReplaysFromAnotherClient(t *testing.T) {
	// The end-to-end consequence: a coat captured from one tool must serve a
	// request made by a different one.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"users":[]}`))
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
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	req, err := http.NewRequest("GET", p.URL()+"/api/users", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("User-Agent", "curl/8.7.1")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("capture request failed: %v", err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	files, err := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("expected a captured coat, glob err %v files %v", err, files)
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

	replayReq, err := http.NewRequest("GET", "http://"+addr+"/api/users", nil)
	if err != nil {
		t.Fatalf("failed to build replay request: %v", err)
	}
	replayReq.Header.Set("User-Agent", "some-other-tool/2.0")
	replayed, err := httpClient.Do(replayReq)
	if err != nil {
		t.Fatalf("replay request failed: %v", err)
	}
	defer func() { _ = replayed.Body.Close() }()

	if replayed.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(replayed.Body)
		t.Fatalf("replaying a captured coat from a different client got %d: %s", replayed.StatusCode, body)
	}
}

func TestProxy_CapturedBracketedPathValidatesAndReplays(t *testing.T) {
	// request.uri is recorded from the path the capture was taken from, and a
	// path carrying a glob metacharacter stops meaning that path the moment the
	// matcher reads it. /api/items[abc] is a character class matching
	// /api/itemsa, /api/itemsb and /api/itemsc -- everything except the request
	// that produced the coat. An unbalanced bracket fails louder: /api/items[]
	// does not compile, so `trenchcoat validate` rejects a coat this tool wrote.
	// Bracketed segments are ordinary in filter[name]-style APIs.
	for _, path := range []string{"/api/items[abc]", "/api/items[]"} {
		t.Run(path, func(t *testing.T) {
			const body = "captured"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprint(w, body)
			}))
			defer upstream.Close()

			writeDir := t.TempDir()
			p, err := proxy.New(proxy.Config{
				UpstreamURL:  upstream.URL,
				WriteDir:     writeDir,
				StripHeaders: []string{},
				Dedupe:       "overwrite",
				Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err != nil {
				t.Fatalf("failed to create proxy: %v", err)
			}
			if _, err := p.Start("127.0.0.1:0"); err != nil {
				t.Fatalf("failed to start proxy: %v", err)
			}
			t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

			resp, err := httpClient.Get(p.URL() + path)
			if err != nil {
				t.Fatalf("capture request failed: %v", err)
			}
			_ = resp.Body.Close()
			p.WaitCaptures()

			// LoadPaths validates, so this is `trenchcoat validate` on the capture.
			loaded, errs := coat.LoadPaths([]string{writeDir})
			if len(errs) > 0 {
				t.Fatalf("the captured coat does not validate: %v", errs)
			}
			if len(loaded) != 1 {
				t.Fatalf("expected 1 captured coat, got %d", len(loaded))
			}

			mock := server.New(loaded, server.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			addr, err := mock.Start("127.0.0.1:0")
			if err != nil {
				t.Fatalf("failed to start mock: %v", err)
			}
			t.Cleanup(func() { _ = mock.Shutdown(5 * time.Second) })

			replayed, err := httpClient.Get("http://" + addr + path)
			if err != nil {
				t.Fatalf("replay request failed: %v", err)
			}
			defer func() { _ = replayed.Body.Close() }()

			got, _ := io.ReadAll(replayed.Body)
			if replayed.StatusCode != http.StatusOK || string(got) != body {
				t.Fatalf("replaying %s against its own capture got %d %q, want 200 %q",
					path, replayed.StatusCode, got, body)
			}
		})
	}
}

func TestProxy_DropsHeadersNamedInConnection(t *testing.T) {
	// RFC 9110 7.6.1: the Connection header lists headers that are scoped to
	// this hop only. Filtering just the static hop-by-hop set forwards them
	// anyway, in both directions.
	sawUpstream := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawUpstream <- r.Header.Clone()
		w.Header().Set("Connection", "X-Internal-Backend")
		w.Header().Set("X-Internal-Backend", "pod-7")
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

	req, err := http.NewRequest("GET", p.URL()+"/hop", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Connection", "keep-alive, X-Trace-Token")
	req.Header.Set("X-Trace-Token", "secret")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	p.WaitCaptures()

	upstreamHeaders := <-sawUpstream
	if got := upstreamHeaders.Get("X-Trace-Token"); got != "" {
		t.Errorf("X-Trace-Token was scoped to this hop by the client's Connection header but reached the upstream as %q", got)
	}

	if got := resp.Header.Get("X-Internal-Backend"); got != "" {
		t.Errorf("X-Internal-Backend was scoped to this hop by the upstream's Connection header but reached the client as %q", got)
	}
}

func TestProxy_CaptureOmitsConnectionScopedHeaders(t *testing.T) {
	// A header a peer scopes to this hop via Connection is withheld from the
	// wire in both directions -- so it must not be recorded either. Capturing
	// the request one makes it a replay match constraint for a header the
	// upstream provably never saw; capturing the response one makes the mock
	// serve a header the proxy itself refused to relay, and `Connection: close`
	// then kills keep-alive for every replay client.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "X-Internal-Backend")
		w.Header().Set("X-Internal-Backend", "pod-7")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":1}`))
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
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	req, err := http.NewRequest("GET", p.URL()+"/hop", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Connection", "keep-alive, X-Trace-Token")
	req.Header.Set("X-Trace-Token", "secret")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	p.WaitCaptures()

	captured := readOnlyCoat(t, writeDir)

	if got, ok := captured.Coats[0].Request.Headers["X-Trace-Token"]; ok {
		t.Errorf("captured request headers include X-Trace-Token=%q, which the client scoped to this hop and the upstream never received", got)
	}
	for name := range captured.Coats[0].Response.Headers {
		switch {
		case strings.EqualFold(name, "Connection"),
			strings.EqualFold(name, "Keep-Alive"),
			strings.EqualFold(name, "X-Internal-Backend"):
			t.Errorf("captured response headers include %s, which the proxy withheld from the client", name)
		}
	}
	if got := captured.Coats[0].Response.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type must still be captured, got %q", got)
	}
}

func TestProxy_DedupeSkip_WriteDirContainingGlobMetacharacters(t *testing.T) {
	// The existence check used to interpolate --write-dir into a glob pattern.
	// A directory named "run[1]" made "[1]" a character class, so the check
	// searched "run1", found nothing, and skip silently behaved as overwrite --
	// writing a file per request instead of the one it promises.
	// The response differs per request, so the file's contents say whether the
	// later captures were skipped or wrote over the first. Counting files cannot
	// tell: skip filenames are stable, so a skip that degrades to overwrite
	// still leaves exactly one file behind.
	var mu sync.Mutex
	n := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		body := fmt.Sprintf("response-%d", n)
		mu.Unlock()
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	writeDir := filepath.Join(t.TempDir(), "run[1]")
	if err := os.MkdirAll(writeDir, 0700); err != nil {
		t.Fatal(err)
	}

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
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	for range 3 {
		resp, err := httpClient.Get(p.URL() + "/api/items")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		p.WaitCaptures()
	}

	content, err := os.ReadFile(filepath.Join(writeDir, "GET_api_items_200.yaml"))
	if err != nil {
		t.Fatalf("expected a captured coat: %v", err)
	}
	if !strings.Contains(string(content), "response-1") {
		t.Fatalf("dedupe=skip kept the last capture rather than the first, so the existence check never saw the file it had already written:\n%s", content)
	}
}

func TestProxy_DedupeSkip_FilenameIsStable(t *testing.T) {
	// skip guarantees one file per request, so the name carries no timestamp:
	// a predictable name is what makes a captured fixture referenceable from a
	// test or a script.
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
	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/items")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	p.WaitCaptures()

	want := filepath.Join(writeDir, "GET_api_items_200.yaml")
	if _, err := os.Stat(want); err != nil {
		entries, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
		t.Fatalf("expected the stable name %s, found %v", filepath.Base(want), entries)
	}
}

func TestProxy_BodyFileFailureRemovesTheCoatReferencingIt(t *testing.T) {
	// A coat naming a body file that is not there is worse than no coat: it
	// serves a 500 at replay for a capture the user believes succeeded. If the
	// body cannot be written, the coat pointing at it must not survive either.
	//
	// The body file is made unwritable by pre-creating it as a directory, which
	// os.WriteFile cannot overwrite.
	body := strings.Repeat("x", 8192)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(writeDir, "GET_api_items_200_body.txt"), 0700); err != nil {
		t.Fatal(err)
	}

	p, err := proxy.New(proxy.Config{
		UpstreamURL:       upstream.URL,
		WriteDir:          writeDir,
		StripHeaders:      []string{},
		Dedupe:            "overwrite",
		BodyFileThreshold: 1024,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/items")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	p.WaitCaptures()

	coatPath := filepath.Join(writeDir, "GET_api_items_200.yaml")
	if _, err := os.Stat(coatPath); err == nil {
		content, _ := os.ReadFile(coatPath)
		t.Fatalf("the coat survived a failed body-file write, so replaying it would 500:\n%s", content)
	}
}

func TestProxy_ShutdownBeforeStartIsNotAnError(t *testing.T) {
	// Start assigns httpServer, so a deferred Shutdown after a failed or absent
	// Start would otherwise dereference nil -- turning a reportable error into a
	// panic in the caller's cleanup.
	p, err := proxy.New(proxy.Config{
		UpstreamURL: "http://127.0.0.1:1",
		WriteDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if err := p.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown before Start should be a no-op, got %v", err)
	}
}

func TestProxy_CaptureIsNeverVisiblePartiallyWritten(t *testing.T) {
	// A coat file is read by other processes while the proxy is writing it --
	// `trenchcoat serve --watch` pointed at the same directory is the obvious
	// case, and `trenchcoat validate` the other. os.WriteFile truncates and then
	// writes, so a reader that opens the file in between sees a prefix of the
	// new content, or nothing at all, and a coat file half a response long is
	// not valid YAML.
	//
	// Writing to a temp file and renaming makes the swap atomic: a reader sees
	// either the whole previous capture or the whole new one.
	body := strings.Repeat("A", 256*1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	coatPath := filepath.Join(writeDir, "GET_api_items_200.yaml")

	// Poll the coat file the way a watching process would, and record any read
	// that produced something unparseable or short.
	stop := make(chan struct{})
	var readerWG sync.WaitGroup
	var mu sync.Mutex
	var reads, bad int
	var firstBad string

	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}

			content, err := os.ReadFile(coatPath)
			if err != nil {
				continue // Not written yet; a missing file is not a torn one.
			}

			var parsed coat.File
			problem := ""
			switch {
			case yaml.Unmarshal(content, &parsed) != nil:
				problem = "not valid YAML"
			case len(parsed.Coats) != 1:
				problem = fmt.Sprintf("%d coats", len(parsed.Coats))
			case parsed.Coats[0].Response == nil:
				problem = "no response"
			case len(parsed.Coats[0].Response.Body) != len(body):
				problem = fmt.Sprintf("body is %d bytes, want %d", len(parsed.Coats[0].Response.Body), len(body))
			}

			mu.Lock()
			reads++
			if problem != "" {
				bad++
				if firstBad == "" {
					firstBad = problem
				}
			}
			mu.Unlock()
		}
	}()

	// Keep the captures overlapping rather than waiting for each one: the window
	// a reader can fall into is the write itself, so back-to-back rewrites are
	// what make it reachable at all.
	var clients sync.WaitGroup
	for range 8 {
		clients.Add(1)
		go func() {
			defer clients.Done()
			for range 40 {
				resp, err := httpClient.Get(p.URL() + "/api/items")
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}
	clients.Wait()
	p.WaitCaptures()

	close(stop)
	readerWG.Wait()

	mu.Lock()
	defer mu.Unlock()
	if reads == 0 {
		t.Fatal("the reader never managed to read the coat file, so this proves nothing")
	}
	if bad > 0 {
		t.Fatalf("%d of %d reads saw a partially written coat file (first: %s); a watching process would load a broken fixture",
			bad, reads, firstBad)
	}
}
