package httputil_test

import (
	"io"
	"strings"
	"testing"

	"github.com/yesdevnull/trenchcoat/internal/httputil"
)

func TestReconstitutedBody(t *testing.T) {
	original := "hello world"
	captured := []byte("hello ")
	remaining := io.NopCloser(strings.NewReader("world"))

	body := httputil.ReconstitutedBody(captured, remaining)

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("got %q, want %q", string(got), original)
	}

	if err := body.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestReconstitutedBodyEmpty(t *testing.T) {
	remaining := io.NopCloser(strings.NewReader("full body"))
	body := httputil.ReconstitutedBody(nil, remaining)

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "full body" {
		t.Errorf("got %q, want %q", string(got), "full body")
	}
}
