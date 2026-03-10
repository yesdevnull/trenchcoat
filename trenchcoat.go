// Package trenchcoat provides a programmatic API for using Trenchcoat
// as a mock HTTP server in Go test suites.
//
// Example usage:
//
//	func TestMyAPI(t *testing.T) {
//	    srv := trenchcoat.NewServer(
//	        trenchcoat.WithCoat(trenchcoat.Coat{
//	            Name: "get-users",
//	            Request: trenchcoat.Request{
//	                Method: "GET",
//	                URI:    "/api/v1/users",
//	            },
//	            Response: &trenchcoat.Response{
//	                Code: 200,
//	                Headers: map[string]string{"Content-Type": "application/json"},
//	                Body: `{"users": []}`,
//	            },
//	        }),
//	    )
//	    srv.Start(t) // registers t.Cleanup to stop the server
//
//	    resp, err := http.Get(srv.URL + "/api/v1/users")
//	    // ... assert response ...
//	}
package trenchcoat

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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

// CapturedRequest records details of an incoming request that matched a coat.
type CapturedRequest struct {
	Method   string
	URI      string
	RawQuery string
	Header   http.Header
	Body     string
}

// Coat is an individual request/response mock definition.
type Coat = coat.Coat

// Request defines the matching criteria for an incoming HTTP request.
type Request = coat.Request

// Response defines the mock response to return.
type Response = coat.Response

// QueryField represents the query field which can be either a string or a map.
type QueryField = coat.QueryField

// StringPtr returns a pointer to s. It is a convenience helper for constructing
// Request literals with a body constraint.
func StringPtr(s string) *string {
	return coat.StringPtr(s)
}

// Server wraps the internal Trenchcoat server for use in tests.
type Server struct {
	// URL is the base URL of the running server (e.g. "http://127.0.0.1:12345").
	// Set after Start() is called.
	URL string

	// TLSClient is an *http.Client configured to trust the server's TLS
	// certificate. It is only guaranteed to be set when WithSelfSignedTLS is
	// used and may be nil in other configurations (including WithTLS).
	TLSClient *http.Client

	coats       []coat.LoadedCoat
	loadErrs    []error
	inner       *server.Server
	verbose     bool
	tlsCertFile string
	tlsKeyFile  string
	selfSigned  bool
}

// Option configures the Server.
type Option func(*Server)

// WithCoat adds a coat definition to the server.
func WithCoat(c Coat) Option {
	return func(s *Server) {
		s.coats = append(s.coats, coat.LoadedCoat{Coat: c})
	}
}

// WithCoats adds multiple coat definitions to the server.
func WithCoats(coats ...Coat) Option {
	return func(s *Server) {
		for _, c := range coats {
			s.coats = append(s.coats, coat.LoadedCoat{Coat: c})
		}
	}
}

// WithCoatFile loads coats from a file path.
func WithCoatFile(path string) Option {
	return func(s *Server) {
		loaded, errs := coat.LoadPaths([]string{path})
		s.coats = append(s.coats, loaded...)
		s.loadErrs = append(s.loadErrs, errs...)
	}
}

// WithVerbose enables verbose request logging.
func WithVerbose() Option {
	return func(s *Server) {
		s.verbose = true
	}
}

// WithTLS configures the server to use TLS with the given certificate and key files.
func WithTLS(certFile, keyFile string) Option {
	return func(s *Server) {
		s.tlsCertFile = certFile
		s.tlsKeyFile = keyFile
	}
}

// WithSelfSignedTLS generates an ephemeral self-signed certificate and
// configures the server to use TLS. After Start is called, TLSClient is
// set to an *http.Client that trusts the generated certificate.
func WithSelfSignedTLS() Option {
	return func(s *Server) {
		s.selfSigned = true
	}
}

// NewServer creates a new test server with the given options.
// Call Start(t) to begin serving.
func NewServer(opts ...Option) *Server {
	s := &Server{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Start begins serving on an ephemeral port. It registers a cleanup
// function on t to shut down the server when the test completes.
func (s *Server) Start(t testing.TB) {
	t.Helper()

	for _, err := range s.loadErrs {
		t.Fatalf("trenchcoat: coat loading error: %v", err)
	}

	s.inner = server.New(s.coats, server.Config{
		Verbose:     s.verbose,
		RecordCalls: true, // always record for test assertions
	})

	// Validate that both cert and key are provided together.
	if (s.tlsCertFile != "") != (s.tlsKeyFile != "") {
		t.Fatalf("trenchcoat: WithTLS requires both certFile and keyFile; got certFile=%q, keyFile=%q", s.tlsCertFile, s.tlsKeyFile)
	}

	// WithTLS and WithSelfSignedTLS are mutually exclusive.
	if s.selfSigned && s.tlsCertFile != "" {
		t.Fatalf("trenchcoat: WithTLS and WithSelfSignedTLS are mutually exclusive; use one or the other")
	}

	useTLS := s.tlsCertFile != "" || s.selfSigned

	if s.selfSigned {
		certFile, keyFile, pool := generateSelfSignedCert(t)
		s.tlsCertFile = certFile
		s.tlsKeyFile = keyFile
		s.TLSClient = &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: pool,
				},
			},
		}
	}

	if useTLS {
		// Eagerly validate the cert/key pair so the test fails immediately
		// with a clear message rather than silently failing during handshake.
		if _, err := tls.LoadX509KeyPair(s.tlsCertFile, s.tlsKeyFile); err != nil {
			t.Fatalf("trenchcoat: invalid TLS cert/key pair: %v", err)
		}

		addr, err := s.inner.StartTLS("127.0.0.1:0", s.tlsCertFile, s.tlsKeyFile)
		if err != nil {
			t.Fatalf("trenchcoat: failed to start TLS server: %v", err)
		}
		s.URL = "https://" + addr
	} else {
		addr, err := s.inner.Start("127.0.0.1:0")
		if err != nil {
			t.Fatalf("trenchcoat: failed to start server: %v", err)
		}
		s.URL = "http://" + addr
	}

	t.Cleanup(func() {
		s.Stop()
	})
}

// generateSelfSignedCert creates an ephemeral self-signed certificate for testing.
func generateSelfSignedCert(t testing.TB) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("trenchcoat: failed to generate TLS key: %v", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		t.Fatalf("trenchcoat: failed to generate serial number: %v", err)
	}

	now := time.Now()

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    now.Add(-1 * time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("trenchcoat: failed to create certificate: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("trenchcoat: failed to create cert file: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}
	if err := certOut.Close(); err != nil {
		t.Fatalf("trenchcoat: failed to close cert file: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("trenchcoat: failed to marshal key: %v", err)
	}
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("trenchcoat: failed to create key file: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	if err := keyOut.Close(); err != nil {
		t.Fatalf("trenchcoat: failed to close key file: %v", err)
	}

	pool = x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})); !ok {
		t.Fatalf("trenchcoat: failed to append certificate to pool")
	}

	return certFile, keyFile, pool
}

// Stop shuts down the server.
func (s *Server) Stop() {
	if s.inner != nil {
		_ = s.inner.Shutdown(5 * time.Second)
	}
}

// AssertCalled fails the test if the named coat was never called.
func (s *Server) AssertCalled(t testing.TB, name string) {
	t.Helper()
	s.requireStarted(t)
	if s.inner.CallCount(name) == 0 {
		t.Errorf("trenchcoat: expected coat %q to have been called, but it was not", name)
	}
}

// AssertCalledN fails the test if the named coat was not called exactly n times.
func (s *Server) AssertCalledN(t testing.TB, name string, n int) {
	t.Helper()
	s.requireStarted(t)
	got := s.inner.CallCount(name)
	if got != n {
		t.Errorf("trenchcoat: expected coat %q to have been called %d time(s), got %d", name, n, got)
	}
}

// AssertNotCalled fails the test if the named coat was called.
func (s *Server) AssertNotCalled(t testing.TB, name string) {
	t.Helper()
	s.requireStarted(t)
	if got := s.inner.CallCount(name); got > 0 {
		t.Errorf("trenchcoat: expected coat %q not to have been called, but it was called %d time(s)", name, got)
	}
}

// Requests returns all captured requests for the named coat.
func (s *Server) Requests(name string) []CapturedRequest {
	if s.inner == nil {
		return nil
	}
	internal := s.inner.Calls(name)
	out := make([]CapturedRequest, len(internal))
	for i, cr := range internal {
		captured := CapturedRequest{
			Method:   cr.Method,
			URI:      cr.URI,
			RawQuery: cr.RawQuery,
			Body:     cr.Body,
		}
		if cr.Header != nil {
			captured.Header = cr.Header.Clone()
		}
		out[i] = captured
	}
	return out
}

// ResetCalls clears all recorded call data.
func (s *Server) ResetCalls() {
	if s.inner == nil {
		return
	}
	s.inner.ResetCalls()
}

// requireStarted fails the test if the server has not been started.
func (s *Server) requireStarted(t testing.TB) {
	t.Helper()
	if s.inner == nil {
		t.Fatalf("trenchcoat: server has not been started; call Start(t) before using assertions")
	}
}
