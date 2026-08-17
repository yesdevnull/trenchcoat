package proxy_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/proxy"
)

// tlsUpstream is an HTTPS test upstream that records the SNI server name sent by
// the client on each handshake.
type tlsUpstream struct {
	*httptest.Server

	mu  sync.Mutex
	sni string
}

func (u *tlsUpstream) lastSNI() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sni
}

// startTLSUpstream starts an HTTPS upstream presenting a self-signed certificate
// valid only for certHost, listening on 127.0.0.1 — the hostname mismatch the
// proxy's --tls-server-name override exists to handle.
//
// The certificate is made trustworthy by installing it as a root on
// http.DefaultTransport, which the proxy clones for upstream requests. That
// keeps the test off the system root store, and pins the requirement that the
// proxy preserves the default transport's TLS settings when applying the
// override.
func startTLSUpstream(t *testing.T, certHost string, h http.Handler) *tlsUpstream {
	t.Helper()

	cert, leaf := generateCertForHost(t, certHost)

	up := &tlsUpstream{Server: httptest.NewUnstartedServer(h)}
	up.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			up.mu.Lock()
			up.sni = chi.ServerName
			up.mu.Unlock()
			return nil, nil
		},
	}
	// Discard the upstream's own handshake logging: tests assert on the error the
	// proxy reports, which is the side that performs certificate verification.
	up.Config.ErrorLog = log.New(io.Discard, "", 0)
	up.StartTLS()
	t.Cleanup(up.Close)

	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	dt, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is %T, want *http.Transport", http.DefaultTransport)
	}
	previous := dt.TLSClientConfig
	dt.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	t.Cleanup(func() { dt.TLSClientConfig = previous })

	return up
}

// generateCertForHost creates a self-signed certificate whose only subject
// alternative name is host. It deliberately carries no IP SANs, so connecting
// by IP address fails hostname verification.
func generateCertForHost(t *testing.T, host string) (tls.Certificate, *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{host},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key, Leaf: leaf}, leaf
}

func TestProxy_TLSServerName_AllowsMismatchedUpstreamHostname(t *testing.T) {
	upstream := startTLSUpstream(t, "upstream.test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"from": "upstream"}`))
	}))

	p, err := proxy.New(proxy.Config{
		UpstreamURL:   upstream.URL,
		WriteDir:      t.TempDir(),
		TLSServerName: "upstream.test",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

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
		t.Fatalf("expected 200, got %d (body: %s)", resp.StatusCode, body)
	}
	if string(body) != `{"from": "upstream"}` {
		t.Fatalf("expected upstream body, got %s", body)
	}
}

func TestProxy_TLSServerName_IsSentAsSNI(t *testing.T) {
	upstream := startTLSUpstream(t, "upstream.test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	p, err := proxy.New(proxy.Config{
		UpstreamURL:   upstream.URL,
		WriteDir:      t.TempDir(),
		TLSServerName: "upstream.test",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/v1/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if got := upstream.lastSNI(); got != "upstream.test" {
		t.Fatalf("expected upstream to observe SNI %q, got %q", "upstream.test", got)
	}
}

func TestProxy_TLSHostnameMismatch_FailsWithoutTLSServerName(t *testing.T) {
	upstream := startTLSUpstream(t, "upstream.test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	var logs bytes.Buffer
	p, err := proxy.New(proxy.Config{
		UpstreamURL: upstream.URL,
		WriteDir:    t.TempDir(),
		Logger:      slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if _, err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get(p.URL() + "/api/v1/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for certificate hostname mismatch, got %d", resp.StatusCode)
	}
	if !strings.Contains(logs.String(), "cannot validate certificate for 127.0.0.1") {
		t.Fatalf("expected a certificate hostname verification error to be logged, got: %s", logs.String())
	}
}
