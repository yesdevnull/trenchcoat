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

	"github.com/yesdevnull/trenchcoat/internal/proxy"
)

func TestProxy_New_EmptyURL(t *testing.T) {
	_, err := proxy.New(proxy.Config{UpstreamURL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected 'must not be empty' error, got: %v", err)
	}
}

func TestProxy_New_InvalidScheme(t *testing.T) {
	_, err := proxy.New(proxy.Config{UpstreamURL: "ftp://example.com"})
	if err == nil {
		t.Fatal("expected error for invalid scheme")
	}
	if !strings.Contains(err.Error(), "scheme must be http or https") {
		t.Fatalf("expected scheme error, got: %v", err)
	}
}

func TestProxy_New_MissingHost(t *testing.T) {
	_, err := proxy.New(proxy.Config{UpstreamURL: "http://"})
	if err == nil {
		t.Fatal("expected error for missing host")
	}
	if !strings.Contains(err.Error(), "host must not be empty") {
		t.Fatalf("expected host error, got: %v", err)
	}
}

func TestProxy_Addr_BeforeStart(t *testing.T) {
	p, err := proxy.New(proxy.Config{UpstreamURL: "http://localhost:9999"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Addr() != "" {
		t.Fatalf("expected empty addr before Start(), got %q", p.Addr())
	}
}

func TestProxy_ForwardsRequestBody(t *testing.T) {
	var receivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := readAllBody(r)
		receivedBody = string(b)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    writeDir,
		Dedupe:      "overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := http.Post(p.URL()+"/api/data", "application/json", strings.NewReader(`{"key":"value"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	if receivedBody != `{"key":"value"}` {
		t.Fatalf("expected body forwarded to upstream, got %q", receivedBody)
	}
}

func TestProxy_CapturesQueryString(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    writeDir,
		Dedupe:      "overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := http.Get(p.URL() + "/search?foo=bar&baz=qux")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if len(files) == 0 {
		t.Fatal("expected captured coat file")
	}
	content, _ := os.ReadFile(files[0])
	if !strings.Contains(string(content), "foo=bar") {
		t.Fatalf("expected query string in coat, got: %s", content)
	}
}

func TestProxy_UpstreamUnreachable(t *testing.T) {
	// Start and immediately close a server to get a deterministically unreachable port.
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := closedServer.URL
	closedServer.Close()

	p, err := proxy.New(proxy.Config{
		UpstreamURL: unreachableURL,
		WriteDir:    t.TempDir(),
		Dedupe:      "overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := http.Get(p.URL() + "/test")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Fatalf("expected 502 for unreachable upstream, got %d", resp.StatusCode)
	}
}

func TestProxy_Dedupe_Overwrite(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
		_, _ = w.Write([]byte("response-" + strings.Repeat("x", callCount)))
	}))
	defer upstream.Close()

	writeDir := t.TempDir()
	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    writeDir,
		Dedupe:      "overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	// Make same request twice.
	resp, _ := http.Get(p.URL() + "/test")
	_ = resp.Body.Close()
	p.WaitCaptures()

	resp2, _ := http.Get(p.URL() + "/test")
	_ = resp2.Body.Close()
	p.WaitCaptures()

	files, _ := filepath.Glob(filepath.Join(writeDir, "*.yaml"))
	if len(files) != 1 {
		t.Fatalf("expected exactly 1 file with overwrite dedup, got %d", len(files))
	}

	// File should have the second response's content.
	content, _ := os.ReadFile(files[0])
	if !strings.Contains(string(content), "response-xx") {
		t.Fatalf("expected overwritten content from second request, got: %s", content)
	}
}

func TestProxy_Verbose(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    t.TempDir(),
		Verbose:     true,
		Dedupe:      "overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := http.Get(p.URL() + "/test")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	p.WaitCaptures()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func readAllBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(r.Body)
}
