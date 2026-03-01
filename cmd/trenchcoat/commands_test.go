package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
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
	cmd.SetArgs([]string{filepath.Join(t.TempDir(), "missing.yaml")})

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
	ctx, cancel := context.WithCancel(context.Background())

	cmd := newServeCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--port", "0"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Cancel the context to stop the server; runServe waits on ctx after Start().
	cancel()

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

	ctx, cancel := context.WithCancel(context.Background())

	cmd := newServeCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--coats", coatFile, "--port", "0", "--verbose"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Cancel the context to stop the server; runServe waits on ctx after Start().
	cancel()

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

	ctx, cancel := context.WithCancel(context.Background())

	cmd := newServeCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--coats", coatFile, "--port", "0", "--watch"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Cancel the context to stop the server; runServe waits on ctx after Start().
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for serve to stop")
	}
}

func TestServeCmd_TLS(t *testing.T) {
	// Generate self-signed certificate.
	certDir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, certDir)

	// Create a coat file.
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "tls-coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "tls-test"
    request:
      uri: "/secure"
    response:
      code: 200
      body: "secure-ok"
`)

	// Pick a free port (runServe doesn't expose the listen address).
	port := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := newServeCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{
		"--coats", coatFile,
		"--port", fmt.Sprintf("%d", port),
		"--tls-cert", certFile,
		"--tls-key", keyFile,
	})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Build TLS client and wait for server readiness.
	client := newTLSClient(t, certFile)
	url := fmt.Sprintf("https://127.0.0.1:%d/secure", port)
	waitForTLS(t, client, url)

	// Make the actual HTTPS request and assert.
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("TLS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "secure-ok" {
		t.Fatalf("expected body %q, got %q", "secure-ok", string(body))
	}

	// Shut down cleanly.
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error from serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for serve to stop")
	}
}

func TestBinary_TLS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary test in short mode")
	}

	// Build the binary.
	binary := filepath.Join(t.TempDir(), "trenchcoat")
	build := exec.Command("go", "build", "-o", binary, "./")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, out)
	}

	// Generate self-signed certificate.
	certDir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, certDir)

	// Create a coat file.
	dir := t.TempDir()
	coatFile := filepath.Join(dir, "coat.yaml")
	writeTestFile(t, coatFile, `
coats:
  - name: "binary-tls"
    request:
      uri: "/hello"
    response:
      code: 200
      body: "hello-tls"
`)

	port := freePort(t)

	// Start the binary as a subprocess.
	cmd := exec.Command(binary, "serve",
		"--coats", coatFile,
		"--port", fmt.Sprintf("%d", port),
		"--tls-cert", certFile,
		"--tls-key", keyFile,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start binary: %v", err)
	}
	t.Cleanup(func() {
		// Ensure process is killed if test fails early.
		_ = cmd.Process.Signal(os.Kill)
		_ = cmd.Wait()
	})

	// Wait for TLS readiness.
	client := newTLSClient(t, certFile)
	url := fmt.Sprintf("https://127.0.0.1:%d/hello", port)
	waitForTLS(t, client, url)

	// Make an HTTPS request and assert.
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("TLS request to binary failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "hello-tls" {
		t.Fatalf("expected body %q, got %q", "hello-tls", string(body))
	}

	// Graceful shutdown.
	signalAndWait(t, cmd, &stderr, 5*time.Second)
}

func TestBinary_ProxyThenServe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary test in short mode")
	}

	// Shared client with explicit timeout to avoid hanging in
	// CI environments without outbound network access or with slow DNS.
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Phase 0: Check network availability.
	checkResp, err := httpClient.Get("https://jsonplaceholder.typicode.com/posts/1")
	if err != nil {
		t.Skipf("skipping: jsonplaceholder.typicode.com unreachable: %v", err)
	}
	_ = checkResp.Body.Close()
	if checkResp.StatusCode != 200 {
		t.Skipf("skipping: jsonplaceholder.typicode.com returned status %d", checkResp.StatusCode)
	}

	// Phase 1: Build binary.
	binary := filepath.Join(t.TempDir(), "trenchcoat")
	build := exec.Command("go", "build", "-o", binary, "./")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, out)
	}

	// Phase 2: Start proxy mode.
	writeDir := t.TempDir()
	proxyPort := freePort(t)
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)

	proxyCmd := exec.Command(binary, "proxy",
		"https://jsonplaceholder.typicode.com",
		"--port", fmt.Sprintf("%d", proxyPort),
		"--write-dir", writeDir,
		"--dedupe", "overwrite",
	)
	var proxyStderr bytes.Buffer
	proxyCmd.Stderr = &proxyStderr

	if err := proxyCmd.Start(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() {
		_ = proxyCmd.Process.Signal(os.Kill)
		_ = proxyCmd.Wait()
	})

	waitForHTTP(t, proxyURL+"/posts/1")

	// Phase 3: Make the real request through proxy and capture the body.
	resp, err := httpClient.Get(proxyURL + "/posts/1")
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	capturedBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read proxy response body: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected proxy response 200, got %d", resp.StatusCode)
	}
	if len(capturedBody) == 0 {
		t.Fatal("proxy response body is empty")
	}

	// Validate captured body is valid JSON with expected keys.
	var post map[string]any
	if err := json.Unmarshal(capturedBody, &post); err != nil {
		t.Fatalf("proxy response is not valid JSON: %v\nbody: %s", err, capturedBody)
	}
	for _, key := range []string{"userId", "id", "title", "body"} {
		if _, ok := post[key]; !ok {
			t.Fatalf("proxy response JSON missing key %q: %s", key, capturedBody)
		}
	}

	// Phase 4: Stop proxy (flushes captures via Shutdown → captures.Wait).
	signalAndWait(t, proxyCmd, &proxyStderr, 10*time.Second)

	// Phase 5: Validate captured coat via binary.
	validateCmd := exec.Command(binary, "validate", writeDir)
	if out, err := validateCmd.CombinedOutput(); err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out)
	}

	// Phase 6: Verify captured coat file structure.
	coatFile := filepath.Join(writeDir, "GET_posts_1_200.yaml")
	parsed, err := coat.ParseFile(coatFile)
	if err != nil {
		t.Fatalf("failed to parse captured coat file: %v", err)
	}
	if len(parsed.Coats) != 1 {
		t.Fatalf("expected 1 coat, got %d", len(parsed.Coats))
	}
	c := parsed.Coats[0]
	if c.Request.Method != "GET" {
		t.Fatalf("expected request method GET, got %q", c.Request.Method)
	}
	if c.Request.URI != "/posts/1" {
		t.Fatalf("expected request URI /posts/1, got %q", c.Request.URI)
	}
	if c.Response == nil {
		t.Fatal("expected response in coat, got nil")
	}
	if c.Response.Code != 200 {
		t.Fatalf("expected response code 200, got %d", c.Response.Code)
	}
	if c.Response.Body == "" {
		t.Fatal("expected non-empty response body in coat")
	}

	// Verify coat body is valid JSON with expected keys.
	var coatPost map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(c.Response.Body)), &coatPost); err != nil {
		t.Fatalf("coat response body is not valid JSON: %v\nbody: %s", err, c.Response.Body)
	}
	for _, key := range []string{"userId", "id", "title", "body"} {
		if _, ok := coatPost[key]; !ok {
			t.Fatalf("coat response body JSON missing key %q", key)
		}
	}

	// Phase 7: Start serve mode with captured coat.
	servePort := freePort(t)
	serveURL := fmt.Sprintf("http://127.0.0.1:%d", servePort)

	serveCmd := exec.Command(binary, "serve",
		"--coats", writeDir,
		"--port", fmt.Sprintf("%d", servePort),
	)
	var serveStderr bytes.Buffer
	serveCmd.Stderr = &serveStderr

	if err := serveCmd.Start(); err != nil {
		t.Fatalf("failed to start serve: %v", err)
	}
	t.Cleanup(func() {
		_ = serveCmd.Process.Signal(os.Kill)
		_ = serveCmd.Wait()
	})

	waitForHTTP(t, serveURL+"/posts/1")

	// Phase 8: Assert serve response matches proxy capture.
	serveResp, err := httpClient.Get(serveURL + "/posts/1")
	if err != nil {
		t.Fatalf("serve request failed: %v", err)
	}
	serveBody, err := io.ReadAll(serveResp.Body)
	_ = serveResp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read serve response body: %v", err)
	}
	if serveResp.StatusCode != 200 {
		t.Fatalf("expected serve response 200, got %d", serveResp.StatusCode)
	}

	// Normalize trailing whitespace from YAML block scalar round-trip.
	want := strings.TrimRight(string(capturedBody), "\n")
	got := strings.TrimRight(string(serveBody), "\n")
	if got != want {
		t.Fatalf("serve response body does not match proxy capture.\nwant: %s\ngot:  %s", want, got)
	}

	// Validate served body is also valid JSON.
	var servePost map[string]any
	if err := json.Unmarshal(serveBody, &servePost); err != nil {
		t.Fatalf("serve response is not valid JSON: %v\nbody: %s", err, serveBody)
	}

	// Phase 9: Stop serve.
	signalAndWait(t, serveCmd, &serveStderr, 5*time.Second)
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
	ctx, cancel := context.WithCancel(context.Background())

	cmd := newProxyCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"http://localhost:9999", "--port", "0", "--write-dir", t.TempDir()})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Cancel the context to stop the proxy; runProxy waits on ctx after Start().
	cancel()

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
	ctx, cancel := context.WithCancel(context.Background())

	cmd := newProxyCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"http://localhost:9999", "--port", "0", "--write-dir", t.TempDir(), "--verbose", "--log-format", "json"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	// Cancel the context to stop the proxy; runProxy waits on ctx after Start().
	cancel()

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
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	done := make(chan struct{})
	go func() {
		watchCoats(ctx, logger, nil, []string{nonExistent})
		close(done)
	}()

	// Cancel to stop the watcher cleanly.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for watchCoats to return")
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

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() {
		watchCoats(ctx, logger, srv, []string{dir, coatFile})
		close(done)
	}()

	// Cancel the context to stop the watcher cleanly.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for watchCoats to return")
	}
}

// --- helpers ---

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

// freePort binds an ephemeral port, records it, and closes the listener.
// The port is momentarily available for reuse — standard Go test pattern.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind ephemeral port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to close ephemeral listener: %v", err)
	}
	return port
}

// generateSelfSignedCert creates a self-signed ECDSA P256 certificate and
// private key for localhost/127.0.0.1, writing PEM files to dir.
// Duplicated from internal/server/tls_test.go (package server_test is not
// importable from package main).
func generateSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}
	if err := certOut.Close(); err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal EC private key: %v", err)
	}
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	if err := keyOut.Close(); err != nil {
		t.Fatal(err)
	}

	return certFile, keyFile
}

// newTLSClient returns an HTTP client that trusts the given PEM certificate file.
func newTLSClient(t *testing.T, certFile string) *http.Client {
	t.Helper()
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("failed to read cert file: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to append cert to pool")
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool,
			},
		},
	}
}

// waitForTLS polls the given HTTPS URL until it responds or the timeout is reached.
func waitForTLS(t *testing.T, client *http.Client, url string) {
	t.Helper()
	var lastErr error
	for range 50 {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("TLS server not ready after polling: %v", lastErr)
}

// waitForHTTP polls the given HTTP URL until it responds or the timeout is reached.
func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for range 50 {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("HTTP server not ready after polling: %v", lastErr)
}

// signalAndWait sends a signal to a process and waits for it to exit with a timeout.
func signalAndWait(t *testing.T, cmd *exec.Cmd, stderr *bytes.Buffer, timeout time.Duration) {
	t.Helper()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("failed to send SIGINT: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process exited with error: %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(timeout):
		t.Fatal("timeout waiting for process to exit")
	}
}
