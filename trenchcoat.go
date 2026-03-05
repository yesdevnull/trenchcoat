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
	"net/http"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

// CapturedRequest records details of an incoming request that matched a coat.
type CapturedRequest struct {
	Method string
	URI    string
	Header http.Header
	Body   string
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

	coats    []coat.LoadedCoat
	loadErrs []error
	inner    *server.Server
	verbose  bool
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
		Verbose: s.verbose,
	})

	addr, err := s.inner.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("trenchcoat: failed to start server: %v", err)
	}

	s.URL = "http://" + addr

	t.Cleanup(func() {
		s.Stop()
	})
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
	if s.inner.CallCount(name) == 0 {
		t.Errorf("trenchcoat: expected coat %q to have been called, but it was not", name)
	}
}

// AssertCalledN fails the test if the named coat was not called exactly n times.
func (s *Server) AssertCalledN(t testing.TB, name string, n int) {
	t.Helper()
	got := s.inner.CallCount(name)
	if got != n {
		t.Errorf("trenchcoat: expected coat %q to have been called %d time(s), got %d", name, n, got)
	}
}

// AssertNotCalled fails the test if the named coat was called.
func (s *Server) AssertNotCalled(t testing.TB, name string) {
	t.Helper()
	if got := s.inner.CallCount(name); got > 0 {
		t.Errorf("trenchcoat: expected coat %q not to have been called, but it was called %d time(s)", name, got)
	}
}

// Requests returns all captured requests for the named coat.
func (s *Server) Requests(name string) []CapturedRequest {
	internal := s.inner.Calls(name)
	out := make([]CapturedRequest, len(internal))
	for i, cr := range internal {
		out[i] = CapturedRequest{
			Method: cr.Method,
			URI:    cr.URI,
			Header: cr.Header,
			Body:   cr.Body,
		}
	}
	return out
}

// ResetCalls clears all recorded call data.
func (s *Server) ResetCalls() {
	s.inner.ResetCalls()
}
