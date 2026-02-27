package trenchcoat

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestWithCoat(t *testing.T) {
	srv := NewServer(
		WithCoat(Coat{
			Name:     "test",
			Request:  Request{Method: "GET", URI: "/test"},
			Response: &Response{Code: 200, Body: "hello"},
		}),
	)
	srv.Start(t)
	defer srv.Stop()

	resp, err := http.Get(srv.URL + "/test")
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
	defer srv.Stop()

	resp, err := http.Get(srv.URL + "/a")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/b")
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
	defer srv.Stop()

	resp, err := http.Get(srv.URL + "/from-file")
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
	defer srv.Stop()

	resp, err := http.Get(srv.URL + "/verbose")
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
