package server_test

import (
	"bytes"
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
