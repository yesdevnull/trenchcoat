// Package proxy implements the Trenchcoat proxy capture mode.
// It forwards requests to an upstream server and captures request/response
// pairs as coat files.
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds proxy configuration.
type Config struct {
	UpstreamURL  string
	WriteDir     string
	Filter       string
	StripHeaders []string
	Dedupe       string // overwrite, skip, append
	Verbose      bool
	Logger       *slog.Logger
}

// Proxy is the Trenchcoat proxy capture server.
type Proxy struct {
	config     Config
	logger     *slog.Logger
	httpServer *http.Server
	listener   net.Listener
	upstream   *url.URL
	client     *http.Client

	mu       sync.Mutex
	counters map[string]int // for append dedup mode

	captures sync.WaitGroup
}

// New creates a new Proxy.
func New(cfg Config) (*Proxy, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Dedupe == "" {
		cfg.Dedupe = "overwrite"
	}
	if cfg.WriteDir == "" {
		cfg.WriteDir = "."
	}

	if cfg.UpstreamURL == "" {
		return nil, fmt.Errorf("invalid upstream URL %q: must not be empty", cfg.UpstreamURL)
	}

	upstream, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL %q: %w", cfg.UpstreamURL, err)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return nil, fmt.Errorf("invalid upstream URL %q: scheme must be http or https", cfg.UpstreamURL)
	}
	if upstream.Host == "" {
		return nil, fmt.Errorf("invalid upstream URL %q: host must not be empty", cfg.UpstreamURL)
	}

	p := &Proxy{
		config:   cfg,
		logger:   cfg.Logger,
		upstream: upstream,
		client: &http.Client{
			Transport: http.DefaultTransport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		counters: make(map[string]int),
	}

	return p, nil
}

// Start begins the proxy server.
func (p *Proxy) Start(addr string) (string, error) {
	// Ensure write directory exists.
	if err := os.MkdirAll(p.config.WriteDir, 0755); err != nil {
		return "", fmt.Errorf("creating write directory %s: %w", p.config.WriteDir, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleRequest)

	p.httpServer = &http.Server{Handler: mux}

	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	p.listener = ln

	go func() {
		if err := p.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			p.logger.Error("proxy server error", "error", err)
		}
	}()

	return ln.Addr().String(), nil
}

// Addr returns the address the proxy is listening on.
func (p *Proxy) Addr() string {
	if p.listener != nil {
		return p.listener.Addr().String()
	}
	return ""
}

// URL returns the base URL of the proxy.
func (p *Proxy) URL() string {
	return "http://" + p.Addr()
}

// WaitCaptures blocks until all pending capture goroutines have completed.
func (p *Proxy) WaitCaptures() {
	p.captures.Wait()
}

// Shutdown gracefully stops the proxy, waiting for pending captures to complete.
func (p *Proxy) Shutdown(timeout time.Duration) error {
	p.captures.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.httpServer.Shutdown(ctx)
}

func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Determine if we should capture this request.
	shouldCapture := p.shouldCapture(r.URL.Path)

	// Read and buffer the request body.
	var reqBody []byte
	if r.Body != nil {
		reqBody, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}

	// Forward request to upstream.
	upstreamURL := *p.upstream
	upstreamURL.Path = singleJoiningSlash(upstreamURL.Path, r.URL.Path)
	upstreamURL.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		p.logger.Error("failed to create proxy request", "error", err)
		http.Error(w, "proxy error", http.StatusBadGateway)
		return
	}

	// Copy headers.
	for k, vv := range r.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}

	upstreamResp, err := p.client.Do(proxyReq)
	if err != nil {
		p.logger.Error("upstream request failed", "error", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	// Read upstream response body.
	respBody, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		p.logger.Error("failed to read upstream response", "error", err)
		http.Error(w, "proxy read error", http.StatusBadGateway)
		return
	}

	// Relay response to client.
	for k, vv := range upstreamResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(respBody)

	upstreamDuration := time.Since(start)

	if p.config.Verbose {
		p.logger.Info("proxied request",
			"method", r.Method,
			"uri", r.URL.RequestURI(),
			"upstream_status", upstreamResp.StatusCode,
			"upstream_duration", upstreamDuration.String(),
			"captured", shouldCapture,
		)
	}

	// Capture if applicable.
	if shouldCapture {
		p.captures.Go(func() { p.captureCoat(r, reqBody, upstreamResp, respBody) })
	}
}

func (p *Proxy) shouldCapture(urlPath string) bool {
	if p.config.Filter == "" {
		return true
	}
	matched, err := path.Match(p.config.Filter, urlPath)
	if err != nil {
		p.logger.Error("invalid capture filter pattern", "filter", p.config.Filter, "error", err)
		return false
	}
	return matched
}

func (p *Proxy) captureCoat(r *http.Request, reqBody []byte, resp *http.Response, respBody []byte) {
	// Build coat definition.
	reqHeaders := make(map[string]string)
	for k := range r.Header {
		if !p.isStrippedHeader(k) {
			reqHeaders[k] = r.Header.Get(k)
		}
	}

	respHeaders := make(map[string]string)
	for k := range resp.Header {
		if !p.isStrippedHeader(k) {
			respHeaders[k] = resp.Header.Get(k)
		}
	}

	coatDef := coatFile{
		Coats: []coatEntry{
			{
				Name: fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				Request: coatRequest{
					Method: r.Method,
					URI:    r.URL.Path,
				},
				Response: coatResponse{
					Code:    resp.StatusCode,
					Headers: respHeaders,
					Body:    string(respBody),
				},
			},
		},
	}

	if len(reqHeaders) > 0 {
		coatDef.Coats[0].Request.Headers = reqHeaders
	}

	if r.URL.RawQuery != "" {
		coatDef.Coats[0].Request.Query = r.URL.RawQuery
	}

	// Generate filename.
	filename := p.generateFilename(r.Method, r.URL.Path, resp.StatusCode)
	if filename == "" {
		// Skip dedup — file already exists.
		return
	}

	data, err := yaml.Marshal(coatDef)
	if err != nil {
		p.logger.Error("failed to marshal coat", "error", err)
		return
	}

	fullPath := filepath.Join(p.config.WriteDir, filename)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		p.logger.Error("failed to write coat file", "path", fullPath, "error", err)
	} else if p.config.Verbose {
		p.logger.Info("captured coat file", "path", fullPath)
	}
}

func (p *Proxy) generateFilename(method, path string, status int) string {
	ts := time.Now().Unix()
	sanitised := SanitisePath(path)
	base := fmt.Sprintf("%s_%s_%d", method, sanitised, status)

	p.mu.Lock()
	defer p.mu.Unlock()

	switch p.config.Dedupe {
	case "skip":
		// Check if a file with this base already exists (any naming scheme).
		matches, _ := filepath.Glob(filepath.Join(p.config.WriteDir, base+"*.yaml"))
		if len(matches) > 0 {
			return "" // Signal to skip.
		}
		return fmt.Sprintf("%s_%d.yaml", base, ts)

	case "append":
		counter := p.counters[base]
		p.counters[base] = counter + 1
		if counter == 0 {
			return fmt.Sprintf("%s_%d.yaml", base, ts)
		}
		return fmt.Sprintf("%s_%d_%d.yaml", base, counter+1, ts)

	default: // overwrite
		return fmt.Sprintf("%s.yaml", base)
	}
}

func (p *Proxy) isStrippedHeader(header string) bool {
	for _, h := range p.config.StripHeaders {
		if strings.EqualFold(h, header) {
			return true
		}
	}
	return false
}

// SanitisePath converts a URL path to a filename-safe string.
func SanitisePath(path string) string {
	// Strip leading slash.
	path = strings.TrimPrefix(path, "/")
	// Replace / with _.
	path = strings.ReplaceAll(path, "/", "_")
	// Strip non-alphanumeric characters (except _ and -).
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	path = re.ReplaceAllString(path, "")
	if path == "" {
		path = "root"
	}
	return path
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// Types for YAML serialisation of captured coats.
type coatFile struct {
	Coats []coatEntry `yaml:"coats"`
}

type coatEntry struct {
	Name     string       `yaml:"name"`
	Request  coatRequest  `yaml:"request"`
	Response coatResponse `yaml:"response"`
}

type coatRequest struct {
	Method  string            `yaml:"method"`
	URI     string            `yaml:"uri"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Query   string            `yaml:"query,omitempty"`
}

type coatResponse struct {
	Code    int               `yaml:"code"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty"`
}
