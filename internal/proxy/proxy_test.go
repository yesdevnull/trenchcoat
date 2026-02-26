package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesdevnull/genai-experiments/trenchcoat/internal/proxy"
)

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
	resp, err := http.Get(p.URL() + "/api/v1/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != `{"from": "upstream"}` {
		t.Fatalf("expected upstream body, got %s", body)
	}

	// Wait briefly for async coat file write.
	p.WaitCaptures()

	// Check that a coat file was captured.
	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if len(files) == 0 {
		t.Fatal("expected at least one captured coat file")
	}

	// Read the captured file and verify basic structure.
	content, _ := os.ReadFile(files[0])
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

	req, _ := http.NewRequest("GET", p.URL()+"/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	p.WaitCaptures()

	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if len(files) == 0 {
		t.Fatal("expected captured coat file")
	}

	content, _ := os.ReadFile(files[0])
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
	resp, _ := http.Get(p.URL() + "/api/users")
	_ = resp.Body.Close()

	// Request NOT matching the filter.
	resp2, _ := http.Get(p.URL() + "/health")
	_ = resp2.Body.Close()

	p.WaitCaptures()

	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
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
	resp, _ := http.Get(p.URL() + "/test")
	_ = resp.Body.Close()
	p.WaitCaptures()

	resp2, _ := http.Get(p.URL() + "/test")
	_ = resp2.Body.Close()
	p.WaitCaptures()

	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
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
		resp, _ := http.Get(p.URL() + "/test")
		_ = resp.Body.Close()
		p.WaitCaptures()
	}

	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
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

	resp, _ := http.Post(p.URL()+"/api/v1/users", "application/json", strings.NewReader(`{"name": "test"}`))
	_ = resp.Body.Close()
	p.WaitCaptures()

	files, _ := filepath.Glob(filepath.Join(writeDir, "POST_api_v1_users_201_*.yaml"))
	if len(files) == 0 {
		allFiles, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
		t.Fatalf("expected file matching POST_api_v1_users_201_*.yaml, found: %v", allFiles)
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
	resp, err := http.Get(p.URL() + "/api/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	p.WaitCaptures()

	// File should exist immediately after WaitCaptures returns.
	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if len(files) == 0 {
		t.Fatal("expected captured coat file after WaitCaptures()")
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
