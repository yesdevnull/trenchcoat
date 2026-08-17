// Package proxy implements the Trenchcoat proxy capture mode.
// It forwards requests to an upstream server and captures request/response
// pairs as coat files.
package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// sanitiseRe matches characters that are not filename-safe.
var sanitiseRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// maxBodySize is the maximum number of bytes read from request or response bodies.
const maxBodySize = 10 << 20 // 10 MiB

// hopByHopHeaders are headers that must not be forwarded by proxies.
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authorization": {},
	"Proxy-Authenticate":  {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// Config holds proxy configuration.
type Config struct {
	UpstreamURL       string
	WriteDir          string
	Filter            string
	StripHeaders      []string
	NoHeaders         bool   // omit ALL headers from captured coat files
	Dedupe            string // overwrite, skip, append
	CaptureBody       *bool  // capture request body in coat files; nil defaults to true
	PrettyJSON        bool   // pretty-print JSON response bodies in captured coats
	BodyFileThreshold int    // write bodies larger than this to separate files (0 = always inline)
	NameTemplate      string // custom template for captured coat file names
	TLSServerName     string // verify the upstream certificate against this hostname instead of the upstream URL host
	Verbose           bool
	Logger            *slog.Logger
}

// Proxy is the Trenchcoat proxy capture server.
type Proxy struct {
	config     Config
	logger     *slog.Logger
	httpServer *http.Server
	listener   net.Listener
	upstream   *url.URL
	client     *http.Client

	nameTmpl *template.Template // parsed name template (nil = default naming)

	mu       sync.Mutex
	counters map[string]int // for append dedup mode
	inflight map[string]int // base names currently being written, by capture count

	captures   sync.WaitGroup
	captureSem chan struct{} // bounds concurrent capture goroutines
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

	if cfg.NoHeaders && len(cfg.StripHeaders) > 0 {
		return nil, fmt.Errorf("NoHeaders and StripHeaders are mutually exclusive")
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

	// Clone the default transport with DisableCompression so the proxy does not
	// auto-add Accept-Encoding or auto-decompress responses. This keeps the
	// proxy transparent: compressed responses are relayed as-is to the client.
	var transport *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt != nil {
		transport = dt.Clone()
	} else {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	transport.DisableCompression = true

	// Verify the upstream certificate against an explicit hostname rather than the
	// host in the upstream URL, for upstreams whose certificate is issued for a
	// different name than the address they are served from. This also becomes the
	// SNI name sent to the upstream.
	if cfg.TLSServerName != "" {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		transport.TLSClientConfig.ServerName = cfg.TLSServerName
	}

	var nameTmpl *template.Template
	if cfg.NameTemplate != "" {
		var err error
		nameTmpl, err = template.New("name").Parse(cfg.NameTemplate)
		if err != nil {
			return nil, fmt.Errorf("invalid name template: %w", err)
		}
	}

	p := &Proxy{
		config:   cfg,
		logger:   cfg.Logger,
		upstream: upstream,
		nameTmpl: nameTmpl,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		counters:   make(map[string]int),
		inflight:   make(map[string]int),
		captureSem: make(chan struct{}, 20),
	}

	return p, nil
}

// Start begins the proxy server.
func (p *Proxy) Start(addr string) (string, error) {
	// Ensure write directory exists.
	if err := os.MkdirAll(p.config.WriteDir, 0700); err != nil {
		return "", fmt.Errorf("creating write directory %s: %w", p.config.WriteDir, err)
	}

	p.httpServer = &http.Server{
		Handler:           http.HandlerFunc(p.handleRequest),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	p.listener = ln

	go func() {
		if err := p.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

// Shutdown gracefully stops the proxy, draining HTTP connections within half
// the timeout, then waiting for pending captures with the remaining time.
//
// The HTTP drain must come first. While the server is still accepting,
// handlers keep registering captures, so waiting on the capture group first
// both races with the registration (Add concurrent with Wait is documented
// WaitGroup misuse, and panics) and misses every capture the requests still in
// flight go on to spawn -- silently losing exactly the coat files the user was
// trying to record when they pressed Ctrl-C.
func (p *Proxy) Shutdown(timeout time.Duration) error {
	// Split timeout: half for HTTP drain, half for capture drain.
	httpDrain := timeout / 2
	captureDrain := timeout - httpDrain

	ctx, cancel := context.WithTimeout(context.Background(), httpDrain)
	defer cancel()
	shutdownErr := p.httpServer.Shutdown(ctx)

	// No handler can start now, so the capture group can only shrink.
	done := make(chan struct{})
	go func() {
		p.captures.Wait()
		close(done)
	}()
	select {
	case <-done:
		// All captures finished.
	case <-time.After(captureDrain):
		p.logger.Warn("timed out waiting for pending captures to complete")
	}

	return shutdownErr
}

func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Determine if we should capture this request.
	shouldCapture := p.shouldCapture(r.URL.Path)

	// Read the request body with a hard limit to prevent memory exhaustion.
	// Bodies exceeding maxBodySize are rejected with 413.
	var reqBody []byte
	if r.Body != nil {
		var err error
		reqBody, err = io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
		_ = r.Body.Close()
		if err != nil {
			p.logger.Warn("failed to read request body", "error", err)
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		if len(reqBody) > maxBodySize {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
	}

	// Forward request to upstream.
	//
	// Join the escaped forms as well as the decoded ones. URL.String() prefers
	// RawPath when it is a valid encoding of Path, so without this an encoded
	// separator in the client's path is handed upstream as a real one --
	// /seg%2Fment arrives as /seg/ment and hits a different route than the
	// client asked for. A capture tool that rewrites the request in transit is
	// recording something that never happened.
	upstreamURL := *p.upstream
	upstreamURL.Path = singleJoiningSlash(upstreamURL.Path, r.URL.Path)
	upstreamURL.RawPath = singleJoiningSlash(p.upstream.EscapedPath(), r.URL.EscapedPath())
	upstreamURL.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		p.logger.Error("failed to create proxy request", "error", err)
		http.Error(w, "proxy error", http.StatusBadGateway)
		return
	}

	// Copy headers, filtering hop-by-hop headers.
	for k, vv := range r.Header {
		if isHopByHopHeader(k) {
			continue
		}
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

	// Read upstream response body with size limit.
	respBody, err := io.ReadAll(io.LimitReader(upstreamResp.Body, maxBodySize+1))
	if err != nil {
		p.logger.Error("failed to read upstream response", "error", err)
		http.Error(w, "proxy read error", http.StatusBadGateway)
		return
	}
	if len(respBody) > maxBodySize {
		p.logger.Error("upstream response body too large", "size", len(respBody))
		http.Error(w, "upstream response too large", http.StatusBadGateway)
		return
	}

	// Register the capture before the response reaches the client. Once the
	// client has its response it may immediately call WaitCaptures or Shutdown,
	// and a capture that has not yet joined the WaitGroup is invisible to both.
	if shouldCapture {
		capReq := captureRequest{
			Method:   r.Method,
			URI:      r.URL.Path,
			RawQuery: r.URL.RawQuery,
			Header:   r.Header.Clone(),
			Body:     reqBody,
		}
		p.captures.Go(func() {
			p.captureSem <- struct{}{}
			defer func() { <-p.captureSem }()
			p.captureCoatFromCopy(capReq, upstreamResp, respBody)
		})
	}

	// Relay response to client, filtering hop-by-hop headers.
	for k, vv := range upstreamResp.Header {
		if isHopByHopHeader(k) {
			continue
		}
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
			slog.Duration("upstream_duration", upstreamDuration),
			"captured", shouldCapture,
		)
	}

}

func (p *Proxy) shouldCapture(urlPath string) bool {
	if p.config.Filter == "" {
		return true
	}
	matched, err := doublestar.Match(p.config.Filter, urlPath)
	if err != nil {
		p.logger.Error("invalid capture filter pattern", "filter", p.config.Filter, "error", err)
		return false
	}
	return matched
}

func (p *Proxy) captureCoatFromCopy(req captureRequest, resp *http.Response, respBody []byte) {
	// Decompress response body for human-readable coat capture.
	captureBody := respBody
	decompressed := false
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(bytes.NewReader(respBody))
		if err != nil {
			p.logger.Error("failed to decompress gzip response for capture", "error", err)
		} else {
			plain, err := io.ReadAll(io.LimitReader(gr, maxBodySize))
			_ = gr.Close()
			if err != nil {
				p.logger.Error("failed to read decompressed response for capture", "error", err)
			} else {
				captureBody = plain
				decompressed = true
			}
		}
	}

	// Build coat definition.
	var reqHeaders, respHeaders map[string]string
	if !p.config.NoHeaders {
		reqHeaders = make(map[string]string)
		for k := range req.Header {
			if p.isStrippedHeader(k) || isUncapturedRequestHeader(k) {
				continue
			}
			reqHeaders[k] = req.Header.Get(k)
		}

		respHeaders = make(map[string]string)
		for k := range resp.Header {
			if p.isStrippedHeader(k) || isContentLengthHeader(k) || (decompressed && isContentEncodingHeader(k)) {
				continue
			}
			respHeaders[k] = resp.Header.Get(k)
		}
	}

	responseBody := string(captureBody)

	// Pretty-print JSON response bodies if enabled.
	if p.config.PrettyJSON && json.Valid(captureBody) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, captureBody, "", "  "); err == nil {
			responseBody = pretty.String()
		}
	}

	coatDef := coatFile{
		Coats: []coatEntry{
			{
				Name: fmt.Sprintf("%s %s", req.Method, req.URI),
				Request: coatRequest{
					Method: req.Method,
					URI:    req.URI,
				},
				Response: coatResponse{
					Code:    resp.StatusCode,
					Headers: respHeaders,
					Body:    responseBody,
				},
			},
		},
	}

	if len(reqHeaders) > 0 {
		coatDef.Coats[0].Request.Headers = reqHeaders
	}

	if p.captureBodyEnabled() && len(req.Body) > 0 {
		body := string(req.Body)
		coatDef.Coats[0].Request.Body = &body
	}

	if req.RawQuery != "" {
		coatDef.Coats[0].Request.Query = req.RawQuery
	}

	// Reserve the filename, which both picks it and records that this capture is
	// about to write it. dedupe=skip asks whether a file already exists, and the
	// answer is only meaningful if concurrent captures of the same request can
	// see each other's intent -- otherwise they all look at an empty directory
	// and, if they straddle a second boundary (the name carries a
	// one-second-resolution timestamp), write several files where skip promises
	// one.
	filename, base := p.reserveFilename(req.Method, req.URI, resp.StatusCode)
	if filename == "" {
		// Skip dedup — file already exists or is being written.
		return
	}
	defer p.releaseFilename(base)

	// Write body to a separate file if threshold is exceeded.
	if p.config.BodyFileThreshold > 0 && len(responseBody) > p.config.BodyFileThreshold {
		bodyFileName := strings.TrimSuffix(filename, ".yaml") + "_body.txt"
		bodyFilePath := filepath.Join(p.config.WriteDir, bodyFileName)
		if err := os.WriteFile(bodyFilePath, []byte(responseBody), 0600); err != nil {
			p.logger.Error("failed to write body file", "path", bodyFilePath, "error", err)
			return
		}
		coatDef.Coats[0].Response.Body = ""
		coatDef.Coats[0].Response.BodyFile = bodyFileName
	}

	data, err := yaml.Marshal(coatDef)
	if err != nil {
		p.logger.Error("failed to marshal coat", "error", err)
		return
	}

	fullPath := filepath.Join(p.config.WriteDir, filename)
	if err := os.WriteFile(fullPath, data, 0600); err != nil {
		p.logger.Error("failed to write coat file", "path", fullPath, "error", err)
	} else if p.config.Verbose {
		p.logger.Info("captured coat file", "path", fullPath)
	}
}

// captureRequest holds request data copied from *http.Request before the
// handler returns, so the capture goroutine doesn't reference the original.
type captureRequest struct {
	Method   string
	URI      string
	RawQuery string
	Header   http.Header
	Body     []byte
}

// nameTemplateData provides fields for custom file name templates.
type nameTemplateData struct {
	Method string
	Path   string
	Status int
}

// reserveFilename picks the filename for a capture and records that this
// capture is writing it, returning the filename and the base name to release
// once the write has finished. It returns "" when dedupe=skip and a capture for
// this request already exists on disk or is in flight.
//
// Only the decision is serialised, not the write. Holding p.mu across the write
// would make every capture wait for whichever one is currently touching the
// disk -- so a single slow write stalls all of them, and captureSem's bound of
// 20 concurrent captures becomes unreachable.
func (p *Proxy) reserveFilename(method, urlPath string, status int) (filename, base string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	filename, base = p.generateFilenameLocked(method, urlPath, status)
	if filename == "" {
		return "", ""
	}

	p.inflight[base]++
	return filename, base
}

// releaseFilename records that a capture has finished writing base.
func (p *Proxy) releaseFilename(base string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.inflight[base] <= 1 {
		delete(p.inflight, base)
		return
	}
	p.inflight[base]--
}

// generateFilenameLocked picks the filename for a capture, returning it
// alongside the base name it was derived from. The caller must hold p.mu.
// It returns "" when dedupe=skip and a capture for this request already exists
// on disk or is in flight.
func (p *Proxy) generateFilenameLocked(method, urlPath string, status int) (string, string) {
	ts := time.Now().Unix()
	sanitised := SanitisePath(urlPath)

	var base string
	if p.nameTmpl != nil {
		var buf bytes.Buffer
		data := nameTemplateData{
			Method: method,
			Path:   sanitised,
			Status: status,
		}
		if err := p.nameTmpl.Execute(&buf, data); err != nil {
			p.logger.Error("name template execution failed, using default", "error", err)
			base = fmt.Sprintf("%s_%s_%d", method, sanitised, status)
		} else {
			// Sanitize template output to prevent directory traversal and
			// invalid filenames. Strip path separators and re-apply the
			// same character allowlist used for URL paths.
			rendered := buf.String()
			rendered = strings.ReplaceAll(rendered, "/", "_")
			rendered = strings.ReplaceAll(rendered, "\\", "_")
			rendered = sanitiseRe.ReplaceAllString(rendered, "")
			if rendered == "" {
				rendered = "unnamed"
			}
			base = rendered
		}
	} else {
		base = fmt.Sprintf("%s_%s_%d", method, sanitised, status)
	}

	switch p.config.Dedupe {
	case "skip":
		// Check if a file with this base already exists (any naming scheme).
		matches, _ := filepath.Glob(filepath.Join(p.config.WriteDir, base+"*.yaml"))
		if len(matches) > 0 {
			return "", "" // Signal to skip.
		}
		// A capture already writing this base has not appeared on disk yet, but
		// it is just as much a reason to skip: without this, concurrent captures
		// of one request all see an empty directory and each write a file.
		if p.inflight[base] > 0 {
			return "", ""
		}
		return fmt.Sprintf("%s_%d.yaml", base, ts), base

	case "append":
		counter := p.counters[base]
		p.counters[base] = counter + 1
		if counter == 0 {
			return fmt.Sprintf("%s_%d.yaml", base, ts), base
		}
		return fmt.Sprintf("%s_%d_%d.yaml", base, counter, ts), base

	default: // overwrite
		return fmt.Sprintf("%s.yaml", base), base
	}
}

func (p *Proxy) captureBodyEnabled() bool {
	return p.config.CaptureBody == nil || *p.config.CaptureBody
}

func (p *Proxy) isStrippedHeader(header string) bool {
	for _, h := range p.config.StripHeaders {
		if strings.EqualFold(h, header) {
			return true
		}
	}
	return false
}

// isHopByHopHeader returns true if the header is a hop-by-hop header that
// should not be forwarded by proxies.
func isHopByHopHeader(h string) bool {
	_, ok := hopByHopHeaders[http.CanonicalHeaderKey(h)]
	return ok
}

// clientSpecificRequestHeaders are request headers that describe the client or
// the connection rather than the request, and so are never recorded in a
// captured coat.
//
// Every header a coat records becomes a match constraint the replayed request
// must satisfy. Recording these ties the coat to the tool that captured it: a
// coat taken with curl carries User-Agent: curl/8.7.1 and Accept: */*, and any
// other client replaying the identical request gets a 404. Content-Length and
// Host describe the message and its destination, not its identity, and
// Accept-Encoding is added by the transport rather than the caller.
//
// Headers that genuinely qualify a request -- Content-Type, and custom headers
// such as X-Api-Key -- are still captured.
var clientSpecificRequestHeaders = map[string]struct{}{
	"accept":          {},
	"accept-encoding": {},
	"accept-language": {},
	"content-length":  {},
	"host":            {},
	"user-agent":      {},
}

// isUncapturedRequestHeader reports whether a request header should be left out
// of a captured coat, covering both connection-scoped headers and the
// client-specific set above.
func isUncapturedRequestHeader(h string) bool {
	if isHopByHopHeader(h) {
		return true
	}
	_, ok := clientSpecificRequestHeaders[strings.ToLower(h)]
	return ok
}

// isContentLengthHeader returns true for the Content-Length header, which is
// never recorded in a captured coat. The captured body is what the mock server
// replays, and it differs from the upstream body whenever it is pretty-printed
// or decompressed; net/http derives the correct length when serving, so a
// recorded length can only ever be wrong.
func isContentLengthHeader(h string) bool {
	return strings.EqualFold(h, "content-length")
}

// isContentEncodingHeader returns true for the Content-Encoding header, which
// is stripped from captured coat files when the body has been decompressed for
// readability -- the recorded body is no longer in that encoding.
func isContentEncodingHeader(h string) bool {
	return strings.EqualFold(h, "content-encoding")
}

// SanitisePath converts a URL path to a filename-safe string.
func SanitisePath(path string) string {
	// Strip leading slash.
	path = strings.TrimPrefix(path, "/")
	// Replace / with _.
	path = strings.ReplaceAll(path, "/", "_")
	// Strip non-alphanumeric characters (except _ and -).
	path = sanitiseRe.ReplaceAllString(path, "")
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
	Body    *string           `yaml:"body,omitempty"`
}

type coatResponse struct {
	Code     int               `yaml:"code"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	Body     string            `yaml:"body,omitempty"`
	BodyFile string            `yaml:"body_file,omitempty"`
}
