package server_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

func TestServe_TLS(t *testing.T) {
	certDir := t.TempDir()
	certFile, keyFile := generateSelfSignedCert(t, certDir)

	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "tls-test",
				Request:  coat.Request{Method: "GET", URI: "/secure"},
				Response: &coat.Response{Code: 200, Body: "secure-response"},
			},
		},
	}

	srv := server.New(coats, server.Config{})
	_, err := srv.StartTLS("127.0.0.1:0", certFile, keyFile)
	if err != nil {
		t.Fatalf("failed to start TLS server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Create HTTP client that trusts our self-signed cert.
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("failed to read cert file: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to append cert to pool")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool,
			},
		},
	}

	resp, err := client.Get(srv.TLSUrl() + "/secure")
	if err != nil {
		t.Fatalf("TLS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "secure-response" {
		t.Fatalf("expected secure-response, got %s", body)
	}
}

func TestServe_TLS_CorruptKeyFile(t *testing.T) {
	certDir := t.TempDir()
	certFile, _ := generateSelfSignedCert(t, certDir)

	// Overwrite key with garbage.
	corruptKey := filepath.Join(certDir, "corrupt-key.pem")
	if err := os.WriteFile(corruptKey, []byte("not a valid PEM key"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := server.New(nil, server.Config{})
	_, err := srv.StartTLS("127.0.0.1:0", certFile, corruptKey)
	// StartTLS returns successfully (the goroutine serves in the background),
	// but the TLS handshake will fail because ServeTLS calls tls.LoadX509KeyPair
	// lazily. However, Go's ServeTLS actually validates the cert+key pair eagerly
	// before serving, so the error is logged in the goroutine.
	// Either way, a client connection should fail.
	if err != nil {
		// If StartTLS itself errors, that's also acceptable.
		t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })
		return
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// Attempt a TLS request — it should fail.
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	_, err = client.Get(srv.TLSUrl() + "/test")
	if err == nil {
		t.Fatal("expected TLS handshake error with corrupt key, but request succeeded")
	}
}

func TestServe_TLS_MismatchedCertAndKey(t *testing.T) {
	// Generate two independent key pairs — cert from one, key from another.
	certDir := t.TempDir()
	certFile, _ := generateSelfSignedCert(t, certDir)

	otherDir := t.TempDir()
	_, otherKeyFile := generateSelfSignedCert(t, otherDir)

	srv := server.New(nil, server.Config{})
	_, err := srv.StartTLS("127.0.0.1:0", certFile, otherKeyFile)
	if err != nil {
		t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })
		return // StartTLS can also fail eagerly — acceptable.
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	_, err = client.Get(srv.TLSUrl() + "/test")
	if err == nil {
		t.Fatal("expected TLS error with mismatched cert/key, but request succeeded")
	}
}

func TestServe_TLS_ExpiredCert(t *testing.T) {
	certDir := t.TempDir()
	certFile, keyFile := generateExpiredCert(t, certDir)

	coats := []coat.LoadedCoat{
		{
			Coat: coat.Coat{
				Name:     "expired-test",
				Request:  coat.Request{Method: "GET", URI: "/test"},
				Response: &coat.Response{Code: 200, Body: "ok"},
			},
		},
	}

	srv := server.New(coats, server.Config{})
	_, err := srv.StartTLS("127.0.0.1:0", certFile, keyFile)
	if err != nil {
		t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })
		return
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	// A standard client that validates certificates should reject the expired cert.
	pool := x509.NewCertPool()
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	pool.AppendCertsFromPEM(certPEM)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool,
			},
		},
		Timeout: 2 * time.Second,
	}
	_, err = client.Get(srv.TLSUrl() + "/test")
	if err == nil {
		t.Fatal("expected TLS error for expired certificate, but request succeeded")
	}

	// Server should still be running — a client with InsecureSkipVerify can connect.
	insecureClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := insecureClient.Get(srv.TLSUrl() + "/test")
	if err != nil {
		t.Fatalf("expected server still running with InsecureSkipVerify, got: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// generateExpiredCert creates a certificate that expired in the past.
func generateExpiredCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-2 * time.Hour),
		NotAfter:     time.Now().Add(-1 * time.Hour), // Expired 1 hour ago.
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
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}
	_ = certOut.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	_ = keyOut.Close()

	return certFile, keyFile
}

// generateSelfSignedCert creates a self-signed certificate and key for testing.
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
