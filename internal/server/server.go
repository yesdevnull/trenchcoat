// Package server implements the Trenchcoat mock HTTP server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/matcher"
)

// Server is the Trenchcoat mock HTTP server.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	logger     *slog.Logger
	verbose    bool

	mu      sync.RWMutex
	matcher *matcher.Matcher
	coats   []coat.LoadedCoat
}

// Config holds server configuration.
type Config struct {
	Verbose bool
	Logger  *slog.Logger
}

// New creates a new Server with the given coats and configuration.
func New(loaded []coat.LoadedCoat, cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Server{
		logger:  cfg.Logger,
		verbose: cfg.Verbose,
		matcher: matcher.New(extractCoats(loaded)),
		coats:   loaded,
	}

	s.httpServer = &http.Server{
		Handler: http.HandlerFunc(s.handleRequest),
	}

	return s
}

// extractCoats returns just the Coat values from a slice of LoadedCoats.
func extractCoats(loaded []coat.LoadedCoat) []coat.Coat {
	coats := make([]coat.Coat, len(loaded))
	for i, lc := range loaded {
		coats[i] = lc.Coat
	}
	return coats
}

// Start begins listening on the configured port. It returns the actual
// address the server is listening on (useful for ephemeral ports).
func (s *Server) Start(addr string) (string, error) {
	return s.startListener(addr, func(ln net.Listener) error {
		return s.httpServer.Serve(ln)
	})
}

// StartTLS begins listening with TLS on the configured address.
func (s *Server) StartTLS(addr, certFile, keyFile string) (string, error) {
	return s.startListener(addr, func(ln net.Listener) error {
		return s.httpServer.ServeTLS(ln, certFile, keyFile)
	})
}

func (s *Server) startListener(addr string, serve func(net.Listener) error) (string, error) {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = ln

	go func() {
		if err := serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("server error", "error", err)
		}
	}()

	return ln.Addr().String(), nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// URL returns the base URL of the server (http://host:port).
func (s *Server) URL() string {
	return "http://" + s.Addr()
}

// TLSUrl returns the base TLS URL of the server (https://host:port).
func (s *Server) TLSUrl() string {
	return "https://" + s.Addr()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// Reload replaces the loaded coats and rebuilds the matcher.
func (s *Server) Reload(loaded []coat.LoadedCoat) {
	m := matcher.New(extractCoats(loaded))

	s.mu.Lock()
	s.coats = loaded
	s.matcher = m
	s.mu.Unlock()

	s.logger.Info("coats reloaded", "count", len(loaded))
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	s.mu.RLock()
	m := s.matcher
	allCoats := s.coats
	s.mu.RUnlock()

	result := m.Match(r)

	if result == nil {
		s.writeNoMatch(w, r, start)
		return
	}

	// Handle exhausted sequences.
	if result.Exhausted {
		s.writeSequenceExhausted(w, r, result.Name, start)
		return
	}

	// Determine which response to serve.
	var resp coat.Response
	if result.ResponseIdx >= 0 && len(result.Coat.Responses) > 0 {
		resp = result.Coat.Responses[result.ResponseIdx]
	} else if result.Coat.Response != nil {
		resp = *result.Coat.Response
	} else {
		s.writeNoMatch(w, r, start)
		return
	}

	// Resolve body_file before setting headers so error responses stay clean.
	body := resp.Body
	if resp.BodyFile != "" {
		bodyBytes, err := resolveBodyFile(resp.BodyFile, result.Coat, allCoats)
		if err != nil {
			s.logger.Error("body_file not found", "path", resp.BodyFile, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "body_file not found",
				"path":  resp.BodyFile,
			})
			s.logRequest(r, result.Name, http.StatusInternalServerError, start)
			return
		}
		body = string(bodyBytes)
	}

	// Apply delay.
	if resp.DelayMs > 0 {
		time.Sleep(time.Duration(resp.DelayMs) * time.Millisecond)
	}

	// Set response headers.
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}

	code := resp.Code
	if code == 0 {
		code = http.StatusOK
	}

	w.WriteHeader(code)
	if body != "" {
		_, _ = fmt.Fprint(w, body)
	}

	s.logRequest(r, result.Name, code, start)
}

func (s *Server) writeNoMatch(w http.ResponseWriter, r *http.Request, start time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "no matching coat",
		"method": r.Method,
		"uri":    r.URL.Path,
	})
	s.logRequest(r, "", http.StatusNotFound, start)
}

func (s *Server) writeSequenceExhausted(w http.ResponseWriter, r *http.Request, coatName string, start time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "sequence exhausted",
		"coat":  coatName,
	})
	s.logRequest(r, coatName, http.StatusNotFound, start)
}

func (s *Server) logRequest(r *http.Request, coatName string, status int, start time.Time) {
	if !s.verbose {
		return
	}

	matched := coatName != ""
	s.logger.Info("request",
		"method", r.Method,
		"uri", r.URL.RequestURI(),
		"matched", matched,
		"coat", coatName,
		"status", status,
		slog.Duration("duration", time.Since(start)),
	)
}

// resolveBodyFile resolves a body_file path relative to the coat file's location.
func resolveBodyFile(bodyFile string, c coat.Coat, allCoats []coat.LoadedCoat) ([]byte, error) {
	// Find the file path for this coat, detecting ambiguous matches.
	var coatFilePath string
	for _, lc := range allCoats {
		if lc.Coat.Name == c.Name &&
			lc.Coat.Request.URI == c.Request.URI &&
			lc.Coat.Request.Method == c.Request.Method {
			if coatFilePath == "" {
				coatFilePath = lc.FilePath
			} else if lc.FilePath != coatFilePath {
				return nil, fmt.Errorf("ambiguous coat source for body_file %q: multiple coats match name=%q uri=%q method=%q", bodyFile, c.Name, c.Request.URI, c.Request.Method)
			}
		}
	}

	var resolved string
	if coatFilePath != "" && !filepath.IsAbs(bodyFile) {
		resolved = filepath.Join(filepath.Dir(coatFilePath), bodyFile)
	} else {
		resolved = bodyFile
	}

	return os.ReadFile(resolved)
}
