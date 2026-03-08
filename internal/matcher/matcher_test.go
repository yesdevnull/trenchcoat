package matcher_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/matcher"
)

// --- Exact URI matching ---

func TestMatch_ExactURI(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "exact",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "exact", result.Name)
}

func TestMatch_ExactURI_NoMatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "exact",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/other", nil)
	result := m.Match(req)
	if result != nil {
		t.Fatalf("expected no match, got %q", result.Name)
	}
}

// --- Method matching ---

func TestMatch_MethodMismatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "post-only",
			Request:  coat.Request{Method: "POST", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match for wrong method")
	}
}

func TestMatch_MethodANY(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "any-method",
			Request:  coat.Request{Method: "ANY", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		req := newRequest(t, method, "/api/v1/users", nil)
		result := m.Match(req)
		if result == nil {
			t.Fatalf("expected match for method %s", method)
		}
	}
}

func TestMatch_DefaultMethodIsGET(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "default-get",
			Request:  coat.Request{URI: "/test"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match for GET (default method)")
	}

	req = newRequest(t, "POST", "/test", nil)
	result = m.Match(req)
	if result != nil {
		t.Fatal("expected no match for POST when default method is GET")
	}
}

// --- Glob URI matching ---

func TestMatch_GlobURI(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "glob",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users/123", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected glob match")
	}

	req = newRequest(t, "GET", "/api/v1/users/123/details", nil)
	result = m.Match(req)
	if result != nil {
		t.Fatal("expected no match — glob * does not cross path segments")
	}
}

func TestMatch_GlobQuestionMark(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "glob-question",
			Request:  coat.Request{Method: "GET", URI: "/api/v?/users"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	if m.Match(req) == nil {
		t.Fatal("expected match for v1")
	}

	req = newRequest(t, "GET", "/api/v2/users", nil)
	if m.Match(req) == nil {
		t.Fatal("expected match for v2")
	}
}

// --- Doublestar glob URI matching ---

func TestMatch_DoublestarGlob(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "doublestar",
			Request:  coat.Request{Method: "GET", URI: "/api/**/posts/*"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	// Should match multi-segment paths.
	req := newRequest(t, "GET", "/api/v1/users/123/posts/456", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected doublestar glob match for multi-segment path")
	}
	assertEqual(t, "name", "doublestar", result.Name)

	// Should match single-segment paths too (** matches zero or more).
	req = newRequest(t, "GET", "/api/v1/posts/1", nil)
	result = m.Match(req)
	if result == nil {
		t.Fatal("expected doublestar glob match for single-segment path")
	}
}

func TestMatch_DoublestarGlob_NoMatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "doublestar",
			Request:  coat.Request{Method: "GET", URI: "/api/**/posts"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users/123/comments", nil)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match for path not ending in /posts")
	}
}

func TestMatch_Precedence_ExactBeforeDoublestar(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "doublestar",
			Request:  coat.Request{Method: "GET", URI: "/api/**/users"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "exact",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "exact", result.Name)
}

// --- Regex URI matching ---

func TestMatch_RegexURI(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "regex",
			Request:  coat.Request{Method: "GET", URI: `~/api/v1/users/\d+`},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users/123", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected regex match")
	}

	req = newRequest(t, "GET", "/api/v1/users/abc", nil)
	result = m.Match(req)
	if result != nil {
		t.Fatal("expected no match for non-numeric")
	}
}

// --- Header matching ---

func TestMatch_HeaderSubset(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "with-headers",
			Request: coat.Request{
				Method:  "GET",
				URI:     "/test",
				Headers: map[string]string{"Accept": "application/json"},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", map[string]string{
		"Accept":       "application/json",
		"X-Request-Id": "abc",
	})
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — request has required headers plus extras")
	}
}

func TestMatch_HeaderMissing(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "with-headers",
			Request: coat.Request{
				Method:  "GET",
				URI:     "/test",
				Headers: map[string]string{"Authorization": "Bearer *"},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", nil)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match — required header missing")
	}
}

func TestMatch_HeaderGlob(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "header-glob",
			Request: coat.Request{
				Method:  "GET",
				URI:     "/test",
				Headers: map[string]string{"Authorization": "Bearer *"},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", map[string]string{
		"Authorization": "Bearer abc123xyz",
	})
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — header value matches glob")
	}
}

// --- Query matching (map form) ---

func TestMatch_QueryMap(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "query-map",
			Request: coat.Request{
				Method: "GET",
				URI:    "/search",
				Query:  &coat.QueryField{Map: map[string]string{"page": "1", "limit": "*"}},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/search?page=1&limit=50", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — query params match with glob")
	}
}

func TestMatch_QueryMap_Missing(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "query-map",
			Request: coat.Request{
				Method: "GET",
				URI:    "/search",
				Query:  &coat.QueryField{Map: map[string]string{"page": "1"}},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/search?limit=50", nil)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match — required query param missing")
	}
}

// --- Query matching (string form) ---

func TestMatch_QueryString(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "query-string",
			Request: coat.Request{
				Method: "GET",
				URI:    "/search",
				Query:  &coat.QueryField{Raw: "page=1&limit=10"},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/search?page=1&limit=10", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — raw query string matches")
	}

	req = newRequest(t, "GET", "/search?page=2&limit=10", nil)
	result = m.Match(req)
	if result != nil {
		t.Fatal("expected no match — different query string")
	}
}

// --- Precedence tests ---

func TestMatch_Precedence_ExactBeforeGlob(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "glob",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "exact",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users/123"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users/123", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "exact", result.Name)
}

func TestMatch_Precedence_GlobBeforeRegex(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "regex",
			Request:  coat.Request{Method: "GET", URI: `~/api/v1/users/\d+`},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "glob",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users/123", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "glob", result.Name)
}

func TestMatch_Precedence_MethodSpecificBeforeANY(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "any",
			Request:  coat.Request{Method: "ANY", URI: "/test"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "get",
			Request:  coat.Request{Method: "GET", URI: "/test"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "get", result.Name)
}

func TestMatch_Precedence_MoreQualifiersWin(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "uri-only",
			Request:  coat.Request{Method: "GET", URI: "/test"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name: "uri-with-headers",
			Request: coat.Request{
				Method:  "GET",
				URI:     "/test",
				Headers: map[string]string{"Accept": "application/json"},
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", map[string]string{"Accept": "application/json"})
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "uri-with-headers", result.Name)
}

func TestMatch_Precedence_GlobLongerPrefixWins(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "short-glob",
			Request:  coat.Request{Method: "GET", URI: "/api/*"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "long-glob",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users/*"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users/123", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "long-glob", result.Name)
}

func TestMatch_Precedence_FileOrderTieBreaker(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "first",
			Request:  coat.Request{Method: "GET", URI: "/test"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "second",
			Request:  coat.Request{Method: "GET", URI: "/test"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/test", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "first", result.Name)
}

func TestMatch_Precedence_GlobSameLiteralLen_FileOrder(t *testing.T) {
	// Two glob URIs with identical literal prefix length that both match the
	// same request — tie-breaks by file definition order (first defined wins).
	coats := []coat.Coat{
		{
			Name:     "first-glob",
			Request:  coat.Request{Method: "GET", URI: "/api/*"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "second-glob",
			Request:  coat.Request{Method: "GET", URI: "/api/?"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	// /api/x matches both patterns (same literalLen "/api/" = 5).
	// First-defined coat should win via file-order tie-break.
	req := newRequest(t, "GET", "/api/x", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "first-glob", result.Name)
}

func TestMatch_Precedence_GlobMethodANY_vs_Specific(t *testing.T) {
	// Two globs with same URI pattern — specific method beats ANY.
	coats := []coat.Coat{
		{
			Name:     "any-glob",
			Request:  coat.Request{Method: "ANY", URI: "/api/*"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "get-glob",
			Request:  coat.Request{Method: "GET", URI: "/api/*"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/test", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "get-glob", result.Name)

	// POST should fall back to ANY.
	req = newRequest(t, "POST", "/api/test", nil)
	result = m.Match(req)
	if result == nil {
		t.Fatal("expected a match for POST via ANY")
	}
	assertEqual(t, "name", "any-glob", result.Name)
}

// --- Query matching edge cases ---

func TestMatch_QueryMap_GlobValues(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "glob-query",
			Request: coat.Request{
				Method: "GET",
				URI:    "/search",
				Query:  &coat.QueryField{Map: map[string]string{"page": "*"}},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/search?page=42", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — glob * should match any value")
	}

	req = newRequest(t, "GET", "/search?page=anything-goes", nil)
	result = m.Match(req)
	if result == nil {
		t.Fatal("expected match — glob * should match any value")
	}
}

func TestMatch_QueryMap_MultipleValues(t *testing.T) {
	// When query has multiple values for same key (?tag=a&tag=b),
	// match should succeed if any value matches the pattern.
	coats := []coat.Coat{
		{
			Name: "multi-value",
			Request: coat.Request{
				Method: "GET",
				URI:    "/filter",
				Query:  &coat.QueryField{Map: map[string]string{"tag": "b"}},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/filter?tag=a&tag=b", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — second value 'b' should match")
	}
}

func TestMatch_QueryMap_SpecialChars(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "encoded-query",
			Request: coat.Request{
				Method: "GET",
				URI:    "/search",
				Query:  &coat.QueryField{Map: map[string]string{"q": "hello world"}},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	// URL-encoded space: q=hello%20world.
	req := newRequest(t, "GET", "/search?q=hello%20world", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — URL-decoded value should match")
	}
}

// --- Double-slash (protocol-in-path) URI matching ---

func TestMatch_ExactURI_DoubleSlash(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "protocol-in-path",
			Request:  coat.Request{Method: "GET", URI: "/Path/To/Json/swis://Hostname/Another/Thing"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/Path/To/Json/swis://Hostname/Another/Thing", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match for URI containing double slashes")
	}
	assertEqual(t, "name", "protocol-in-path", result.Name)
}

func TestMatch_ExactURI_DoubleSlash_NoMatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "protocol-in-path",
			Request:  coat.Request{Method: "GET", URI: "/Path/To/Json/swis://Hostname/Another/Thing"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	// A request with single slash should NOT match a coat with double slash.
	req := newRequest(t, "GET", "/Path/To/Json/swis:/Hostname/Another/Thing", nil)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match — single slash should not match double-slash coat")
	}
}

func TestMatch_RegexURI_DoubleSlash(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "regex-protocol",
			Request:  coat.Request{Method: "GET", URI: `~/Path/To/Json/swis://[^/]+/.*`},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/Path/To/Json/swis://Hostname/Another/Thing", nil)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected regex match for URI containing double slashes")
	}
	assertEqual(t, "name", "regex-protocol", result.Name)
}

// --- Request body matching ---

func TestMatch_BodyExact(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "post-create",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   coat.StringPtr(`{"name": "alice"}`),
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice"}`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match when body matches exactly")
	}
	assertEqual(t, "name", "post-create", result.Name)
}

func TestMatch_BodyMismatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "post-create",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   coat.StringPtr(`{"name": "alice"}`),
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "bob"}`)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match when body does not match")
	}
}

func TestMatch_BodyNotSpecified_MatchesAnyBody(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "post-any-body",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `anything here`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected match — coat without body should match any body")
	}
}

func TestMatch_Precedence_BodySpecificityWins(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "generic-post",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
			},
			Response: &coat.Response{Code: 200},
		},
		{
			Name: "specific-post",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   coat.StringPtr(`{"name": "alice"}`),
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice"}`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	assertEqual(t, "name", "specific-post", result.Name)
}

func TestMatch_DifferentBodies_DifferentCoats(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "create-alice",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   coat.StringPtr(`{"name": "alice"}`),
			},
			Response: &coat.Response{Code: 201, Body: "alice created"},
		},
		{
			Name: "create-bob",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   coat.StringPtr(`{"name": "bob"}`),
			},
			Response: &coat.Response{Code: 201, Body: "bob created"},
		},
	}
	m := matcher.New(coats)

	reqAlice := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice"}`)
	result := m.Match(reqAlice)
	if result == nil {
		t.Fatal("expected match for alice")
	}
	assertEqual(t, "name", "create-alice", result.Name)

	reqBob := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "bob"}`)
	result = m.Match(reqBob)
	if result == nil {
		t.Fatal("expected match for bob")
	}
	assertEqual(t, "name", "create-bob", result.Name)
}

// --- Body match modes ---

func TestMatch_BodyMatch_Glob(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "glob-body",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`{"name": "*", "email": "*@example.com"}`),
				BodyMatch: "glob",
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice", "email": "alice@example.com"}`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected glob body match")
	}
	assertEqual(t, "name", "glob-body", result.Name)
}

func TestMatch_BodyMatch_Glob_NoMatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "glob-body",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`{"name": "*"}`),
				BodyMatch: "glob",
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	// Multi-line body won't match single-line glob pattern.
	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, "no match here\nnewline")
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match for non-matching glob body")
	}
}

func TestMatch_BodyMatch_Contains(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "contains-body",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`"name": "alice"`),
				BodyMatch: "contains",
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice", "email": "alice@example.com"}`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected contains body match")
	}
	assertEqual(t, "name", "contains-body", result.Name)
}

func TestMatch_BodyMatch_Contains_NoMatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "contains-body",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`"name": "bob"`),
				BodyMatch: "contains",
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice"}`)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match for non-matching contains body")
	}
}

func TestMatch_BodyMatch_Regex(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "regex-body",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`\{"name":\s*"[a-z]+"\}`),
				BodyMatch: "regex",
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `{"name": "alice"}`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected regex body match")
	}
	assertEqual(t, "name", "regex-body", result.Name)
}

func TestMatch_BodyMatch_Regex_NoMatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "regex-body",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`^\d+$`),
				BodyMatch: "regex",
			},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `not-a-number`)
	result := m.Match(req)
	if result != nil {
		t.Fatal("expected no match for non-matching regex body")
	}
}

func TestMatch_BodyMatch_Exact_Default(t *testing.T) {
	// Verify that the default (empty BodyMatch) still works as exact.
	coats := []coat.Coat{
		{
			Name: "exact-default",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   coat.StringPtr(`exact match`),
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `exact match`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected exact match with empty BodyMatch")
	}

	req2 := newRequestWithBody(t, "POST", "/api/v1/users", nil, `not exact match`)
	result2 := m.Match(req2)
	if result2 != nil {
		t.Fatal("expected no match for different body with exact mode")
	}
}

func TestMatch_BodyMatch_ExplicitExact(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "explicit-exact",
			Request: coat.Request{
				Method:    "POST",
				URI:       "/api/v1/users",
				Body:      coat.StringPtr(`exact`),
				BodyMatch: "exact",
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequestWithBody(t, "POST", "/api/v1/users", nil, `exact`)
	result := m.Match(req)
	if result == nil {
		t.Fatal("expected explicit exact match")
	}
}

// --- MatchVerbose diagnostics ---

func TestMatchVerbose_MethodMismatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "post-only",
			Request:  coat.Request{Method: "POST", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 201},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result, mismatches := m.MatchVerbose(req)
	if result != nil {
		t.Fatal("expected no match")
	}
	if len(mismatches) == 0 {
		t.Fatal("expected at least one mismatch")
	}
	if !strings.Contains(mismatches[0].Reason, "method mismatch") {
		t.Fatalf("expected method mismatch reason, got %q", mismatches[0].Reason)
	}
	assertEqual(t, "coat_name", "post-only", mismatches[0].CoatName)
}

func TestMatchVerbose_URIMismatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "exact-path",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v2/users", nil)
	result, mismatches := m.MatchVerbose(req)
	if result != nil {
		t.Fatal("expected no match")
	}
	if len(mismatches) == 0 {
		t.Fatal("expected at least one mismatch")
	}
	if !strings.Contains(mismatches[0].Reason, "URI mismatch") {
		t.Fatalf("expected URI mismatch reason, got %q", mismatches[0].Reason)
	}
}

func TestMatchVerbose_HeaderMismatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "auth-required",
			Request: coat.Request{
				Method:  "GET",
				URI:     "/api/v1/users",
				Headers: map[string]string{"Authorization": "Bearer *"},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result, mismatches := m.MatchVerbose(req)
	if result != nil {
		t.Fatal("expected no match")
	}
	if len(mismatches) == 0 {
		t.Fatal("expected at least one mismatch")
	}
	if !strings.Contains(mismatches[0].Reason, "header mismatch") {
		t.Fatalf("expected header mismatch reason, got %q", mismatches[0].Reason)
	}
	if !strings.Contains(mismatches[0].Reason, "Authorization") {
		t.Fatalf("expected Authorization in reason, got %q", mismatches[0].Reason)
	}
}

func TestMatchVerbose_QueryMismatch(t *testing.T) {
	coats := []coat.Coat{
		{
			Name: "paginated",
			Request: coat.Request{
				Method: "GET",
				URI:    "/api/v1/users",
				Query:  &coat.QueryField{Map: map[string]string{"page": "1"}},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users?page=2", nil)
	result, mismatches := m.MatchVerbose(req)
	if result != nil {
		t.Fatal("expected no match")
	}
	if len(mismatches) == 0 {
		t.Fatal("expected at least one mismatch")
	}
	if !strings.Contains(mismatches[0].Reason, "query mismatch") {
		t.Fatalf("expected query mismatch reason, got %q", mismatches[0].Reason)
	}
}

func TestMatchVerbose_NearMissOrdering(t *testing.T) {
	// The coat that passes more stages should appear first.
	coats := []coat.Coat{
		{
			Name:     "wrong-method",
			Request:  coat.Request{Method: "POST", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 201},
		},
		{
			Name: "wrong-header",
			Request: coat.Request{
				Method:  "GET",
				URI:     "/api/v1/users",
				Headers: map[string]string{"Authorization": "Bearer *"},
			},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result, mismatches := m.MatchVerbose(req)
	if result != nil {
		t.Fatal("expected no match")
	}
	if len(mismatches) < 2 {
		t.Fatalf("expected at least 2 mismatches, got %d", len(mismatches))
	}
	// "wrong-header" passed method+URI before failing, so it should be first.
	assertEqual(t, "first near-miss", "wrong-header", mismatches[0].CoatName)
	assertEqual(t, "second near-miss", "wrong-method", mismatches[1].CoatName)
}

func TestMatchVerbose_Limit(t *testing.T) {
	// Create more than maxNearMisses coats.
	var coats []coat.Coat
	for i := range 10 {
		coats = append(coats, coat.Coat{
			Name:     fmt.Sprintf("coat-%d", i),
			Request:  coat.Request{Method: "POST", URI: fmt.Sprintf("/path-%d", i)},
			Response: &coat.Response{Code: 200},
		})
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/nonexistent", nil)
	_, mismatches := m.MatchVerbose(req)
	if len(mismatches) > 5 {
		t.Fatalf("expected at most 5 near-misses, got %d", len(mismatches))
	}
}

func TestMatchVerbose_MatchReturnsNoMismatches(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "exact",
			Request:  coat.Request{Method: "GET", URI: "/api/v1/users"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	req := newRequest(t, "GET", "/api/v1/users", nil)
	result, mismatches := m.MatchVerbose(req)
	if result == nil {
		t.Fatal("expected a match")
	}
	if len(mismatches) != 0 {
		t.Fatalf("expected no mismatches on successful match, got %d", len(mismatches))
	}
}

// --- Helpers ---

func newRequest(t *testing.T, method, uri string, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "http://localhost"+uri, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func newRequestWithBody(t *testing.T, method, uri string, headers map[string]string, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "http://localhost"+uri, strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func assertEqual[T comparable](t *testing.T, field string, expected, actual T) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", field, expected, actual)
	}
}
