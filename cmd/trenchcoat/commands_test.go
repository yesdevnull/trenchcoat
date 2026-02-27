package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/server"
)

// --- validate command tests ---

func TestValidateCmd_ValidFile(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "valid.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "test"
    request:
      uri: "/test"
    response:
      code: 200
      body: "ok"
`)

	cmd := newValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{coatFile})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(out.String(), "all coat files are valid") {
		t.Fatalf("expected success message, got: %s", out.String())
	}
}

func TestValidateCmd_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "invalid.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "no-uri"
    response:
      code: 200
`)

	cmd := newValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{coatFile})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid coat")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("expected validation failed error, got: %v", err)
	}
}

func TestValidateCmd_NonExistentFile(t *testing.T) {
	cmd := newValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"/no/such/file.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestValidateCmd_Directory(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.yaml"), `
coats:
  - name: "a"
    request:
      uri: "/a"
    response:
      code: 200
`)
	writeTestFile(t, filepath.Join(dir, "b.yaml"), `
coats:
  - name: "b"
    request:
      uri: "/b"
    response:
      code: 201
`)

	cmd := newValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{dir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateCmd_NoArgs(t *testing.T) {
	cmd := newValidateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

// --- newLogger tests ---

func TestNewLogger_Text(t *testing.T) {
	logger := newLogger("text")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	logger.Info("test message")
}

func TestNewLogger_JSON(t *testing.T) {
	logger := newLogger("json")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	logger.Info("test message")
}

func TestNewLogger_Default(t *testing.T) {
	logger := newLogger("unknown")
	if logger == nil {
		t.Fatal("expected non-nil logger for unknown format (should default to text)")
	}
	logger.Info("test message")
}

// --- serve command tests ---

func TestServeCmd_TLSCertWithoutKey(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "test"
    request:
      uri: "/test"
    response:
      code: 200
`)

	cmd := newServeCmd()
	cmd.SetArgs([]string{"--coats", coatFile, "--tls-cert", "/some/cert.pem"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when tls-cert provided without tls-key")
	}
	if !strings.Contains(err.Error(), "must be provided together") {
		t.Fatalf("expected 'must be provided together' error, got: %v", err)
	}
}

func TestServeCmd_TLSKeyWithoutCert(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "test"
    request:
      uri: "/test"
    response:
      code: 200
`)

	cmd := newServeCmd()
	cmd.SetArgs([]string{"--coats", coatFile, "--tls-key", "/some/key.pem"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when tls-key provided without tls-cert")
	}
	if !strings.Contains(err.Error(), "must be provided together") {
		t.Fatalf("expected 'must be provided together' error, got: %v", err)
	}
}

func TestServeCmd_NoCoats(t *testing.T) {
	// Start serve with no coats on a random port. It will start successfully
	// and then we send SIGINT to unblock it.
	cmd := newServeCmd()
	cmd.SetArgs([]string{"--port", "0"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Give the server time to start.
	time.Sleep(100 * time.Millisecond)

	// Send SIGINT to unblock.
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for serve to stop")
	}
}

func TestServeCmd_WithCoats(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "test"
    request:
      uri: "/test"
    response:
      code: 200
      body: "ok"
`)

	cmd := newServeCmd()
	cmd.SetArgs([]string{"--coats", coatFile, "--port", "0", "--verbose"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	time.Sleep(100 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for serve to stop")
	}
}

func TestServeCmd_WithWatch(t *testing.T) {
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "test"
    request:
      uri: "/test"
    response:
      code: 200
`)

	cmd := newServeCmd()
	cmd.SetArgs([]string{"--coats", coatFile, "--port", "0", "--watch"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	time.Sleep(200 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for serve to stop")
	}
}

// --- proxy command tests ---

func TestProxyCmd_InvalidDedupe(t *testing.T) {
	cmd := newProxyCmd()
	cmd.SetArgs([]string{"http://localhost:9999", "--dedupe", "random"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid dedupe value")
	}
	if !strings.Contains(err.Error(), "invalid --dedupe value") {
		t.Fatalf("expected dedupe validation error, got: %v", err)
	}
}

func TestProxyCmd_NoArgs(t *testing.T) {
	cmd := newProxyCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestProxyCmd_InvalidUpstreamURL(t *testing.T) {
	cmd := newProxyCmd()
	cmd.SetArgs([]string{"ftp://not-http"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid upstream URL scheme")
	}
}

func TestProxyCmd_StartAndStop(t *testing.T) {
	cmd := newProxyCmd()
	cmd.SetArgs([]string{"http://localhost:9999", "--port", "0", "--write-dir", t.TempDir()})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	time.Sleep(100 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for proxy to stop")
	}
}

func TestProxyCmd_VerboseMode(t *testing.T) {
	cmd := newProxyCmd()
	cmd.SetArgs([]string{"http://localhost:9999", "--port", "0", "--write-dir", t.TempDir(), "--verbose", "--log-format", "json"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	time.Sleep(100 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for proxy to stop")
	}
}

// --- watchCoats tests ---

func TestWatchCoats_NonExistentPaths(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Should not panic even with non-existent paths.
	done := make(chan struct{})
	go func() {
		watchCoats(logger, nil, []string{"/no/such/path"})
		close(done)
	}()
	select {
	case <-done:
		// Returned quickly — that's fine.
	case <-time.After(1 * time.Second):
		// Running (watching) — that's fine too.
	}
}

func TestWatchCoats_WithDirectoryAndFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "test"
    request:
      uri: "/test"
    response:
      code: 200
`)

	// Create a real server so the watcher can call Reload without panicking.
	srv := server.New(nil, server.Config{Logger: logger})
	_, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	done := make(chan struct{})
	go func() {
		watchCoats(logger, srv, []string{dir, coatFile})
		close(done)
	}()
	// Let it run briefly, checking it doesn't crash.
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		// Still running — that's fine for a watcher.
	}
}

// --- helpers ---

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}
