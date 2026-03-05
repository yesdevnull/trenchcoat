// Package matcher implements the request matching engine for Trenchcoat.
// It matches incoming HTTP requests against loaded coat definitions using
// exact, glob, and regex URI matching with precedence ordering.
package matcher

import (
	"bytes"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/yesdevnull/trenchcoat/internal/coat"
)

// maxBodyMatchSize is the maximum request body size (in bytes) that the matcher
// will read for body matching. Bodies larger than this are not eligible for
// body-based matching, so any body-constrained coat will never match such a
// request (although the full body is still restored for downstream use).
const maxBodyMatchSize = 1 << 20 // 1 MiB

// uriMatchType defines how a URI pattern is matched.
type uriMatchType int

const (
	uriExact uriMatchType = iota
	uriGlob
	uriRegex
)

// entry is a compiled coat with pre-computed matching metadata.
type entry struct {
	coat        coat.Coat
	index       int // original definition order
	uriType     uriMatchType
	regex       *regexp.Regexp // only for regex URIs
	literalLen  int            // length of literal prefix for glob patterns
	specificity int            // number of qualifiers (headers + query)
	method      string         // effective method (uppercased, defaulted to GET)
	methodIsANY bool

	// Sequence state for stateful responses.
	seqMu      sync.Mutex
	seqCounter int
}

// MatchResult contains the matched coat and which response to serve.
type MatchResult struct {
	Name        string
	Coat        coat.Coat
	ResponseIdx int  // index into Responses for sequence coats, -1 for singular
	Exhausted   bool // true if sequence is exhausted (once mode)
}

// Matcher matches HTTP requests to coat definitions.
type Matcher struct {
	entries []*entry
}

// New creates a Matcher from the given coats.
func New(coats []coat.Coat) *Matcher {
	entries := make([]*entry, 0, len(coats))
	for i, c := range coats {
		e := &entry{
			coat:  c,
			index: i,
		}

		// Determine URI match type.
		if strings.HasPrefix(c.Request.URI, "~/") {
			e.uriType = uriRegex
			pattern := strings.TrimPrefix(c.Request.URI, "~")
			re, err := regexp.Compile("^" + pattern + "$")
			if err != nil {
				// Skip coats with invalid regex (should be caught by validation).
				continue
			}
			e.regex = re
		} else if strings.ContainsAny(c.Request.URI, "*?") {
			e.uriType = uriGlob
			// Compute literal prefix length (characters before first wildcard).
			for _, ch := range c.Request.URI {
				if ch == '*' || ch == '?' {
					break
				}
				e.literalLen++
			}
		} else {
			e.uriType = uriExact
		}

		// Determine effective method.
		method := strings.ToUpper(c.Request.Method)
		if method == "" {
			method = "GET"
		}
		e.method = method
		e.methodIsANY = method == "ANY"

		// Compute specificity: count of qualifiers (headers + query + body presence).
		if len(c.Request.Headers) > 0 {
			e.specificity += len(c.Request.Headers)
		}
		if c.Request.Query != nil {
			if c.Request.Query.Map != nil {
				e.specificity += len(c.Request.Query.Map)
			} else {
				e.specificity++
			}
		}
		if c.Request.Body != "" {
			e.specificity++
		}

		entries = append(entries, e)
	}

	return &Matcher{entries: entries}
}

// Match finds the best matching coat for an incoming request.
// Returns nil if no coat matches.
//
// If a candidate coat that passed method/URI/header/query checks specifies a
// request body, the request body is read and buffered lazily. The request body
// is replaced with a new reader so it remains available.
func (m *Matcher) Match(req *http.Request) *MatchResult {
	type candidate struct {
		entry *entry
		score matchScore
	}

	// Lazily read the request body only if needed for body matching.
	// Bounded to maxBodyMatchSize to avoid unbounded memory allocation.
	var reqBody []byte
	var bodyRead bool
	var bodyReadErr bool
	getBody := func() ([]byte, bool) {
		if bodyRead {
			return reqBody, bodyReadErr
		}
		bodyRead = true
		if req.Body != nil {
			origBody := req.Body

			// Read up to maxBodyMatchSize+1 bytes so we can detect truncation.
			limited := io.LimitReader(origBody, maxBodyMatchSize+1)
			allRead, err := io.ReadAll(limited)
			if err != nil {
				bodyReadErr = true
			}

			// If we read more than maxBodyMatchSize bytes, treat it as too large
			// for body matching, but still restore the full body for downstream use.
			if len(allRead) > maxBodyMatchSize {
				bodyReadErr = true
				reqBody = allRead[:maxBodyMatchSize]
			} else {
				reqBody = allRead
			}

			// Reconstitute req.Body as the bytes already read plus the remaining
			// unread original body so downstream handlers see the full body, and
			// ensure Close() still delegates to the original body's Close().
			req.Body = struct {
				io.Reader
				io.Closer
			}{
				Reader: io.MultiReader(bytes.NewReader(allRead), origBody),
				Closer: origBody,
			}
		}
		return reqBody, bodyReadErr
	}

	var candidates []candidate

	for _, e := range m.entries {
		if !matchesMethod(e, req.Method) {
			continue
		}
		if !matchesURI(e, req.URL.Path) {
			continue
		}
		if !matchesHeaders(e, req.Header) {
			continue
		}
		if !matchesQuery(e, req.URL.RawQuery, req.URL.Query()) {
			continue
		}
		if !matchesBody(e, getBody) {
			continue
		}

		candidates = append(candidates, candidate{
			entry: e,
			score: computeScore(e),
		})
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort by score descending (best match first).
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score.betterThan(candidates[j].score)
	})

	best := candidates[0].entry
	result := &MatchResult{
		Name: best.coat.Name,
		Coat: best.coat,
	}

	// Handle sequence responses.
	if len(best.coat.Responses) > 0 {
		best.seqMu.Lock()
		defer best.seqMu.Unlock()

		idx := best.seqCounter
		seq := best.coat.Sequence
		if seq == "" {
			seq = "cycle"
		}

		if seq == "once" && idx >= len(best.coat.Responses) {
			result.ResponseIdx = -1
			result.Exhausted = true
			return result
		}

		if seq == "cycle" {
			idx = idx % len(best.coat.Responses)
		}

		best.seqCounter++
		result.ResponseIdx = idx
	} else {
		result.ResponseIdx = -1
	}

	return result
}

// ResetSequences resets all sequence counters (e.g. on hot reload).
func (m *Matcher) ResetSequences() {
	for _, e := range m.entries {
		e.seqMu.Lock()
		e.seqCounter = 0
		e.seqMu.Unlock()
	}
}

// matchScore represents the sorting criteria for match precedence.
type matchScore struct {
	uriType     uriMatchType // exact(0) > glob(1) > regex(2) — lower is better
	specificity int          // higher is better
	literalLen  int          // for glob: higher is better
	methodANY   bool         // specific method beats ANY
	defOrder    int          // lower is better (first defined wins)
}

func computeScore(e *entry) matchScore {
	return matchScore{
		uriType:     e.uriType,
		specificity: e.specificity,
		literalLen:  e.literalLen,
		methodANY:   e.methodIsANY,
		defOrder:    e.index,
	}
}

func (a matchScore) betterThan(b matchScore) bool {
	// 1. Exact URI > Glob > Regex.
	if a.uriType != b.uriType {
		return a.uriType < b.uriType
	}
	// 2. More qualifiers win.
	if a.specificity != b.specificity {
		return a.specificity > b.specificity
	}
	// 3. For globs: longer literal prefix wins.
	if a.uriType == uriGlob && a.literalLen != b.literalLen {
		return a.literalLen > b.literalLen
	}
	// 4. Specific method beats ANY.
	if a.methodANY != b.methodANY {
		return !a.methodANY
	}
	// 5. File definition order (lower index wins).
	return a.defOrder < b.defOrder
}

func matchesMethod(e *entry, method string) bool {
	return e.methodIsANY || e.method == method
}

func matchesURI(e *entry, reqPath string) bool {
	switch e.uriType {
	case uriExact:
		return e.coat.Request.URI == reqPath
	case uriGlob:
		matched, _ := path.Match(e.coat.Request.URI, reqPath)
		return matched
	case uriRegex:
		return e.regex.MatchString(reqPath)
	}
	return false
}

func matchesHeaders(e *entry, reqHeaders http.Header) bool {
	for key, pattern := range e.coat.Request.Headers {
		values := reqHeaders.Values(key)
		if len(values) == 0 {
			return false
		}
		// Check if any header value matches the glob pattern.
		matched := false
		for _, v := range values {
			if globMatch(pattern, v) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func matchesQuery(e *entry, rawQuery string, queryValues map[string][]string) bool {
	q := e.coat.Request.Query
	if q == nil {
		return true
	}

	// String form: literal match against the full raw query string.
	if q.Raw != "" {
		return q.Raw == rawQuery
	}

	// Map form: subset match with glob on values.
	if q.Map != nil {
		for key, pattern := range q.Map {
			values, ok := queryValues[key]
			if !ok || len(values) == 0 {
				return false
			}
			matched := false
			for _, v := range values {
				if globMatch(pattern, v) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}

	return true
}

func matchesBody(e *entry, getBody func() ([]byte, bool)) bool {
	if e.coat.Request.Body == "" {
		return true // No body constraint — matches anything.
	}
	body, readErr := getBody()
	if readErr {
		return false // Treat read errors as non-match.
	}
	return string(body) == e.coat.Request.Body
}

// globMatch performs simple glob matching on a string value.
// Supports * (any characters) and ? (single character).
func globMatch(pattern, value string) bool {
	matched, _ := path.Match(pattern, value)
	return matched
}
