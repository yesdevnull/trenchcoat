package matcher_test

import (
	"sync"
	"testing"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/matcher"
)

func TestMatch_Sequence_Cycle(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:    "cycle",
			Request: coat.Request{Method: "GET", URI: "/health"},
			Responses: []coat.Response{
				{Code: 503, Body: "down"},
				{Code: 200, Body: "up"},
			},
			Sequence: "cycle",
		},
	}
	m := matcher.New(coats)

	// First request → response 0.
	req := newRequest(t, "GET", "/health", nil)
	r := m.Match(req)
	if r == nil {
		t.Fatal("expected match")
	}
	assertEqual(t, "idx-0", 0, r.ResponseIdx)

	// Second request → response 1.
	r = m.Match(newRequest(t, "GET", "/health", nil))
	assertEqual(t, "idx-1", 1, r.ResponseIdx)

	// Third request → cycles back to 0.
	r = m.Match(newRequest(t, "GET", "/health", nil))
	assertEqual(t, "idx-2-cycle", 0, r.ResponseIdx)
}

func TestMatch_Sequence_Once_Exhausted(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:    "once",
			Request: coat.Request{Method: "GET", URI: "/once"},
			Responses: []coat.Response{
				{Code: 200, Body: "first"},
			},
			Sequence: "once",
		},
	}
	m := matcher.New(coats)

	// First request: normal.
	r := m.Match(newRequest(t, "GET", "/once", nil))
	if r == nil {
		t.Fatal("expected match")
	}
	assertEqual(t, "idx", 0, r.ResponseIdx)
	if r.Exhausted {
		t.Fatal("should not be exhausted yet")
	}

	// Second request: exhausted.
	r = m.Match(newRequest(t, "GET", "/once", nil))
	if r == nil {
		t.Fatal("expected match result")
	}
	if !r.Exhausted {
		t.Fatal("expected exhausted")
	}
	assertEqual(t, "exhausted-idx", -1, r.ResponseIdx)
}

func TestMatch_Sequence_DefaultCycle(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:    "default",
			Request: coat.Request{Method: "GET", URI: "/dc"},
			Responses: []coat.Response{
				{Code: 200, Body: "a"},
				{Code: 201, Body: "b"},
			},
			// No explicit sequence — should default to cycle.
		},
	}
	m := matcher.New(coats)

	r := m.Match(newRequest(t, "GET", "/dc", nil))
	assertEqual(t, "first", 0, r.ResponseIdx)
	r = m.Match(newRequest(t, "GET", "/dc", nil))
	assertEqual(t, "second", 1, r.ResponseIdx)
	r = m.Match(newRequest(t, "GET", "/dc", nil))
	assertEqual(t, "third-cycle", 0, r.ResponseIdx)
}

func TestMatch_ResetSequences(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:    "reset-me",
			Request: coat.Request{Method: "GET", URI: "/seq"},
			Responses: []coat.Response{
				{Code: 200, Body: "a"},
				{Code: 201, Body: "b"},
			},
			Sequence: "cycle",
		},
	}
	m := matcher.New(coats)

	// Advance past first response.
	m.Match(newRequest(t, "GET", "/seq", nil))
	r := m.Match(newRequest(t, "GET", "/seq", nil))
	assertEqual(t, "before-reset", 1, r.ResponseIdx)

	// Reset.
	m.ResetSequences()

	// Should be back to 0.
	r = m.Match(newRequest(t, "GET", "/seq", nil))
	assertEqual(t, "after-reset", 0, r.ResponseIdx)
}

func TestNew_InvalidRegexSkipped(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "bad-regex",
			Request:  coat.Request{Method: "GET", URI: "~/api/[bad"},
			Response: &coat.Response{Code: 200},
		},
		{
			Name:     "good",
			Request:  coat.Request{Method: "GET", URI: "/fallback"},
			Response: &coat.Response{Code: 200},
		},
	}
	// Should not panic.
	m := matcher.New(coats)

	// Bad regex coat should be skipped.
	r := m.Match(newRequest(t, "GET", "/api/test", nil))
	if r != nil {
		t.Fatal("expected no match for bad regex coat")
	}

	// Good coat should still work.
	r = m.Match(newRequest(t, "GET", "/fallback", nil))
	if r == nil {
		t.Fatal("expected match for good coat")
	}
	assertEqual(t, "name", "good", r.Name)
}

func TestMatch_EmptyMatcher(t *testing.T) {
	m := matcher.New(nil)
	r := m.Match(newRequest(t, "GET", "/anything", nil))
	if r != nil {
		t.Fatal("expected nil for empty matcher")
	}
}

func TestMatch_ConcurrentSequences(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:    "concurrent",
			Request: coat.Request{Method: "GET", URI: "/concurrent"},
			Responses: []coat.Response{
				{Code: 200},
				{Code: 201},
				{Code: 202},
			},
			Sequence: "cycle",
		},
	}
	m := matcher.New(coats)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := m.Match(newRequest(t, "GET", "/concurrent", nil))
			if r == nil {
				t.Error("expected match")
			}
			if r.ResponseIdx < 0 || r.ResponseIdx > 2 {
				t.Errorf("unexpected response index: %d", r.ResponseIdx)
			}
		}()
	}
	wg.Wait()
}

func TestMatch_SingularResponse(t *testing.T) {
	coats := []coat.Coat{
		{
			Name:     "singular",
			Request:  coat.Request{Method: "GET", URI: "/single"},
			Response: &coat.Response{Code: 200},
		},
	}
	m := matcher.New(coats)

	r := m.Match(newRequest(t, "GET", "/single", nil))
	if r == nil {
		t.Fatal("expected match")
	}
	assertEqual(t, "idx", -1, r.ResponseIdx)
	if r.Exhausted {
		t.Fatal("singular response should not be exhausted")
	}
}
