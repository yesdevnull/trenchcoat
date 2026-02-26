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
	certPEM, _ := os.ReadFile(certFile)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)

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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "secure-response" {
		t.Fatalf("expected secure-response, got %s", body)
	}
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

	certOut, _ := os.Create(certFile)
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}
	if err := certOut.Close(); err != nil {
		t.Fatal(err)
	}

	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyOut, _ := os.Create(keyFile)
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	if err := keyOut.Close(); err != nil {
		t.Fatal(err)
	}

	return certFile, keyFile
}
