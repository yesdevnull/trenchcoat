// Package example_test demonstrates how to use Trenchcoat as a mock
// HTTP server within Go test suites. Each test function shows a different
// feature of Trenchcoat's matching engine.
//
// To run these tests:
//
//	go test -v ./examples/go-tests/
package example_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/yesdevnull/genai-experiments/trenchcoat"
)

// TestBasicMock demonstrates a simple coat serving a JSON response.
// The test starts a Trenchcoat server with a single coat, makes an
// HTTP request, and asserts the response body and status code.
func TestBasicMock(t *testing.T) {
	srv := trenchcoat.NewServer(
		trenchcoat.WithCoat(trenchcoat.Coat{
			Name: "get-users",
			Request: trenchcoat.Request{
				Method: "GET",
				URI:    "/api/v1/users",
			},
			Response: &trenchcoat.Response{
				Code: 200,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: `{"users": [{"id": 1, "name": "Alice"}]}`,
			},
		}),
	)
	srv.Start(t)
	defer srv.Stop()

	resp, err := http.Get(srv.URL + "/api/v1/users")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", resp.Header.Get("Content-Type"))
	}

	body := readBody(t, resp)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	users := result["users"].([]interface{})
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
}

// TestMultipleCoats demonstrates loading several coats together.
// Each test function hits a different endpoint and verifies it returns
// the correct mock response.
func TestMultipleCoats(t *testing.T) {
	srv := trenchcoat.NewServer(
		trenchcoat.WithCoats(
			trenchcoat.Coat{
				Name:     "users-list",
				Request:  trenchcoat.Request{Method: "GET", URI: "/api/users"},
				Response: &trenchcoat.Response{Code: 200, Body: `{"users": []}`},
			},
			trenchcoat.Coat{
				Name:     "health-check",
				Request:  trenchcoat.Request{Method: "GET", URI: "/health"},
				Response: &trenchcoat.Response{Code: 200, Body: `{"status": "ok"}`},
			},
			trenchcoat.Coat{
				Name:     "not-found",
				Request:  trenchcoat.Request{Method: "GET", URI: "/api/missing"},
				Response: &trenchcoat.Response{Code: 404, Body: `{"error": "not found"}`},
			},
		),
	)
	srv.Start(t)
	defer srv.Stop()

	t.Run("users endpoint", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/users")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 200, resp.StatusCode)
		assertBodyContains(t, resp, "users")
	})

	t.Run("health endpoint", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 200, resp.StatusCode)
		assertBodyContains(t, resp, "ok")
	})

	t.Run("missing endpoint", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/missing")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 404, resp.StatusCode)
	})
}

// TestMethodDifferentiation demonstrates GET and POST to the same URI
// returning different responses.
func TestMethodDifferentiation(t *testing.T) {
	srv := trenchcoat.NewServer(
		trenchcoat.WithCoats(
			trenchcoat.Coat{
				Name:    "list-users",
				Request: trenchcoat.Request{Method: "GET", URI: "/api/users"},
				Response: &trenchcoat.Response{
					Code:    200,
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    `{"users": [{"id": 1, "name": "Alice"}]}`,
				},
			},
			trenchcoat.Coat{
				Name:    "create-user",
				Request: trenchcoat.Request{Method: "POST", URI: "/api/users"},
				Response: &trenchcoat.Response{
					Code: 201,
					Headers: map[string]string{
						"Content-Type": "application/json",
						"Location":     "/api/users/2",
					},
					Body: `{"id": 2, "name": "Bob"}`,
				},
			},
		),
	)
	srv.Start(t)
	defer srv.Stop()

	// GET returns the list of users.
	resp, err := http.Get(srv.URL + "/api/users")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "GET status", 200, resp.StatusCode)
	assertBodyContains(t, resp, "Alice")

	// POST creates a new user.
	resp2, err := http.Post(srv.URL+"/api/users", "application/json", nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "POST status", 201, resp2.StatusCode)
	assertEqual(t, "Location header", "/api/users/2", resp2.Header.Get("Location"))
}

// TestHeaderMatching demonstrates a coat that only matches when a
// specific header is present.
func TestHeaderMatching(t *testing.T) {
	srv := trenchcoat.NewServer(
		trenchcoat.WithCoat(trenchcoat.Coat{
			Name: "authenticated-endpoint",
			Request: trenchcoat.Request{
				Method: "GET",
				URI:    "/api/protected",
				// Only matches requests with an Authorization header matching "Bearer *".
				Headers: map[string]string{
					"Authorization": "Bearer *",
				},
			},
			Response: &trenchcoat.Response{
				Code: 200,
				Body: `{"message": "welcome, authorised user"}`,
			},
		}),
	)
	srv.Start(t)
	defer srv.Stop()

	// Request WITH the required header — should match and return 200.
	t.Run("with auth header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/protected", nil)
		req.Header.Set("Authorization", "Bearer my-secret-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 200, resp.StatusCode)
		assertBodyContains(t, resp, "welcome")
	})

	// Request WITHOUT the header — no match, returns 404.
	t.Run("without auth header", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/protected")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 404, resp.StatusCode)
	})
}

// TestResponseSequences demonstrates a coat using responses (plural)
// with cycle mode. Multiple requests return rotating responses.
func TestResponseSequences(t *testing.T) {
	srv := trenchcoat.NewServer(
		trenchcoat.WithCoat(trenchcoat.Coat{
			Name:    "health-sequence",
			Request: trenchcoat.Request{Method: "GET", URI: "/health"},
			Responses: []trenchcoat.Response{
				{Code: 503, Body: "Service Unavailable"},
				{Code: 503, Body: "Service Unavailable"},
				{Code: 200, Body: `{"status": "ok"}`},
			},
			Sequence: "cycle",
		}),
	)
	srv.Start(t)
	defer srv.Stop()

	// First two requests return 503 (simulating a service warming up).
	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("request %d failed: %v", i+1, err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 503, resp.StatusCode)
	}

	// Third request returns 200 (service is ready).
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertEqual(t, "status", 200, resp.StatusCode)

	// Fourth request cycles back to first response (503).
	resp2, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	assertEqual(t, "cycle status", 503, resp2.StatusCode)
}

// TestGlobURIMatching demonstrates a coat with a wildcard URI pattern.
// It matches any path under /api/v1/users/.
func TestGlobURIMatching(t *testing.T) {
	srv := trenchcoat.NewServer(
		trenchcoat.WithCoat(trenchcoat.Coat{
			Name: "user-by-id",
			Request: trenchcoat.Request{
				Method: "GET",
				// Glob pattern: matches any single path segment after /api/v1/users/.
				URI: "/api/v1/users/*",
			},
			Response: &trenchcoat.Response{
				Code:    200,
				Headers: map[string]string{"Content-Type": "application/json"},
				Body:    `{"id": "matched", "name": "Wildcard User"}`,
			},
		}),
	)
	srv.Start(t)
	defer srv.Stop()

	// Various user IDs should all match the glob pattern.
	for _, id := range []string{"1", "42", "alice", "user-123"} {
		t.Run("user/"+id, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/api/v1/users/" + id)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			assertEqual(t, "status", 200, resp.StatusCode)
			assertBodyContains(t, resp, "Wildcard User")
		})
	}

	// Path with additional segments should NOT match (glob * doesn't cross /);
	t.Run("nested path does not match", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/users/42/posts")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertEqual(t, "status", 404, resp.StatusCode)
	})
}

// --- Helpers ---

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	return string(b)
}

func assertEqual[T comparable](t *testing.T, field string, expected, actual T) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", field, expected, actual)
	}
}

func assertBodyContains(t *testing.T, resp *http.Response, substring string) {
	t.Helper()
	body := readBody(t, resp)
	if !contains(body, substring) {
		t.Errorf("expected body to contain %q, got: %s", substring, body)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
