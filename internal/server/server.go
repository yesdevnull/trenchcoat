// Package server implements the Trenchcoat mock HTTP server.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/matcher"
)

// CapturedRequest records details of an incoming request that matched a coat.
type CapturedRequest struct {
	Method string
	URI    string
	Header http.Header
	Body   string
}

// Server is the Trenchcoat mock HTTP server.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	logger     *slog.Logger
	verbose    bool

	mu      sync.RWMutex
	matcher *matcher.Matcher
	coats   []coat.LoadedCoat

	callsMu sync.Mutex
	calls   map[string][]CapturedRequest
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
		calls:   make(map[string][]CapturedRequest),
	}

	s.httpServer = &http.Server{
		Handler: http.HandlerFunc(s.handleRequest),
	}

	return s
}

// extractCoats returns just the Coat values from a slice of LoadedCoat.
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
	return s.startListener(addr, false, func(ln net.Listener) error {
		return s.httpServer.Serve(ln)
	})
}

// StartTLS begins listening with TLS on the configured address.
func (s *Server) StartTLS(addr, certFile, keyFile string) (string, error) {
	return s.startListener(addr, true, func(ln net.Listener) error {
		return s.httpServer.ServeTLS(ln, certFile, keyFile)
	})
}

func (s *Server) startListener(addr string, tls bool, serve func(net.Listener) error) (string, error) {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = ln

	go func() {
		if err := serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("server error", "error", err, "tls", tls)
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

	var result *matcher.MatchResult
	var mismatches []matcher.Mismatch
	if s.verbose {
		result, mismatches = m.MatchVerbose(r)
	} else {
		result = m.Match(r)
	}

	if result == nil {
		s.writeNoMatch(w, r, start, mismatches)
		return
	}

	// Record the matched request for call counting / assertions.
	s.recordCall(result.Name, r)

	// Look up the coat's source file path for logging.
	var coatFilePath string
	if s.verbose {
		for _, lc := range allCoats {
			if lc.Coat.Name == result.Name {
				coatFilePath = lc.FilePath
				break
			}
		}
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
		s.writeNoMatch(w, r, start, nil)
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

	// Render response body templates.
	body = renderTemplate(body, r)

	// Apply delay with optional jitter.
	if resp.DelayMs > 0 || resp.DelayJitterMs > 0 {
		delay := resp.DelayMs
		if resp.DelayJitterMs > 0 {
			delay += rand.IntN(resp.DelayJitterMs)
		}
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
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

	s.logMatchedRequest(r, result, coatFilePath, code, start)
}

func (s *Server) writeNoMatch(w http.ResponseWriter, r *http.Request, start time.Time, mismatches []matcher.Mismatch) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)

	if len(mismatches) > 0 {
		// Log each near-miss.
		for _, mm := range mismatches {
			s.logger.Info("near miss",
				"coat", mm.CoatName,
				"reason", mm.Reason,
			)
		}

		type nearMiss struct {
			CoatName string `json:"coat_name"`
			Reason   string `json:"reason"`
		}
		nearMisses := make([]nearMiss, len(mismatches))
		for i, mm := range mismatches {
			nearMisses[i] = nearMiss{CoatName: mm.CoatName, Reason: mm.Reason}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":       "no matching coat",
			"method":      r.Method,
			"uri":         r.URL.Path,
			"near_misses": nearMisses,
		})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "no matching coat",
			"method": r.Method,
			"uri":    r.URL.Path,
		})
	}
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

func (s *Server) logMatchedRequest(r *http.Request, result *matcher.MatchResult, filePath string, status int, start time.Time) {
	if !s.verbose {
		return
	}

	attrs := []any{
		"method", r.Method,
		"uri", r.URL.RequestURI(),
		"matched", true,
		"coat", result.Name,
		"status", status,
	}

	if filePath != "" {
		attrs = append(attrs, "file", filePath)
	}

	// Log which qualifiers were decisive in the match.
	var qualifiers []string
	if len(result.Coat.Request.Headers) > 0 {
		qualifiers = append(qualifiers, fmt.Sprintf("headers(%d)", len(result.Coat.Request.Headers)))
	}
	if result.Coat.Request.Query != nil {
		qualifiers = append(qualifiers, "query")
	}
	if result.Coat.Request.Body != nil {
		qualifiers = append(qualifiers, "body")
	}
	if len(qualifiers) > 0 {
		attrs = append(attrs, "qualifiers", strings.Join(qualifiers, ","))
	}

	attrs = append(attrs, slog.Duration("duration", time.Since(start)))

	s.logger.Info("request", attrs...)
}

// maxRecordBodySize is the maximum request body size (in bytes) recorded for
// call assertions. Bodies larger than this are truncated with an indicator.
const maxRecordBodySize = 1 << 20 // 1 MiB

// recordCall captures request details for the matched coat.
func (s *Server) recordCall(name string, r *http.Request) {
	cr := CapturedRequest{
		Method: r.Method,
		URI:    r.URL.RequestURI(),
		Header: r.Header.Clone(),
	}

	// Read body for recording with a size limit to prevent DoS.
	// The full body is restored for downstream use.
	if r.Body != nil {
		limited := io.LimitReader(r.Body, maxRecordBodySize+1)
		headBytes, err := io.ReadAll(limited)
		if err == nil && len(headBytes) > maxRecordBodySize {
			cr.Body = string(headBytes[:maxRecordBodySize]) + "...(truncated)"
			// Restore full body: head bytes + remaining unread body.
			r.Body = struct {
				io.Reader
				io.Closer
			}{
				Reader: io.MultiReader(bytes.NewReader(headBytes), r.Body),
				Closer: r.Body,
			}
		} else {
			if err == nil {
				cr.Body = string(headBytes)
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(headBytes))
		}
	}

	s.callsMu.Lock()
	s.calls[name] = append(s.calls[name], cr)
	s.callsMu.Unlock()
}

// CallCount returns the number of times the named coat was matched.
func (s *Server) CallCount(name string) int {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	return len(s.calls[name])
}

// Calls returns all captured requests for the named coat.
func (s *Server) Calls(name string) []CapturedRequest {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	reqs := s.calls[name]
	out := make([]CapturedRequest, len(reqs))
	copy(out, reqs)
	return out
}

// ResetCalls clears all recorded call data.
func (s *Server) ResetCalls() {
	s.callsMu.Lock()
	s.calls = make(map[string][]CapturedRequest)
	s.callsMu.Unlock()
}

// templateData provides request-aware fields for response body templates.
type templateData struct {
	Method string
	Path   string
	Body   string
	query  map[string][]string
	path   string
}

// Query returns the first value for the given query parameter.
func (td templateData) Query(key string) string {
	vals := td.query[key]
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// Segment returns the Nth path segment (0-indexed from root).
// For path "/api/v1/users/123", Segment(0)="api", Segment(3)="123".
func (td templateData) Segment(n int) string {
	parts := strings.Split(strings.TrimPrefix(td.path, "/"), "/")
	if n >= 0 && n < len(parts) {
		return parts[n]
	}
	return ""
}

// renderTemplate parses and executes a Go text/template with request context.
// Returns the original body if it contains no template directives or if parsing fails.
func renderTemplate(body string, r *http.Request) string {
	if !strings.Contains(body, "{{") {
		return body
	}

	tmpl, err := template.New("response").Parse(body)
	if err != nil {
		return body
	}

	// Read request body for template use, capped at maxRecordBodySize to
	// prevent excessive memory usage from large request bodies.
	var reqBody string
	if r.Body != nil {
		limited := io.LimitReader(r.Body, maxRecordBodySize)
		bodyBytes, readErr := io.ReadAll(limited)
		_ = r.Body.Close()
		if readErr == nil {
			reqBody = string(bodyBytes)
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	data := templateData{
		Method: r.Method,
		Path:   r.URL.Path,
		Body:   reqBody,
		query:  r.URL.Query(),
		path:   r.URL.Path,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return body
	}
	return buf.String()
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
