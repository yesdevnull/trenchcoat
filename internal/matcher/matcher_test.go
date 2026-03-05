package matcher_test

import (
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
				Body:   `{"name": "alice"}`,
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
				Body:   `{"name": "alice"}`,
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
				Body:   `{"name": "alice"}`,
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
				Body:   `{"name": "alice"}`,
			},
			Response: &coat.Response{Code: 201, Body: "alice created"},
		},
		{
			Name: "create-bob",
			Request: coat.Request{
				Method: "POST",
				URI:    "/api/v1/users",
				Body:   `{"name": "bob"}`,
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
