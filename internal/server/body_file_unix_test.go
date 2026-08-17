//go:build unix

package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

// body_file resolution is the one place a coat file names a path on disk, so it
// is the one place a coat can try to read something it should not. The guard
// has three layers -- reject absolute paths, reject anything outside the coat
// directory before touching the filesystem, then re-check after resolving
// symlinks -- and only the first had a test.
//
// The symlink layer is the one that has already been wrong once: an earlier
// version pre-checked with os.Stat, which follows symlinks, so a symlink
// pointing outside the coat directory was probed before EvalSymlinks ran.
//
// What these tests pin is the containment decision itself: delete the
// post-EvalSymlinks check and the escaping test serves the target file's
// contents with a 200. They do not pin the Lstat-versus-Stat choice, which
// swaps clean -- that choice governs whether an out-of-tree path is touched at
// all, not whether it is ultimately served, so it needs a different kind of
// test than a status code.
//
// Unix-only: creating symlinks on Windows needs a privilege the CI runner for
// this suite does not have. CI runs on ubuntu-latest.

// serveBodyFileCoat starts a server with a single coat whose body_file is
// bodyFile, resolved relative to a coat file in dir, and returns the response
// to GET /body.
func serveBodyFileCoat(t *testing.T, dir, bodyFile string) *http.Response {
	t.Helper()

	coats := []coat.LoadedCoat{
		{
			FilePath: filepath.Join(dir, "coats.yaml"),
			Coat: coat.Coat{
				Name:     "body-file",
				Request:  coat.Request{Method: "GET", URI: "/body"},
				Response: &coat.Response{Code: 200, BodyFile: bodyFile},
			},
		},
	}

	srv := server.New(coats, server.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	addr, err := srv.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(5 * time.Second) })

	resp, err := httpClient.Get("http://" + addr + "/body")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestResolveBodyFile_SymlinkEscapingTheCoatDirectoryIsRejected(t *testing.T) {
	// A symlink is the interesting case precisely because the path passes every
	// textual check: "leak.json" contains no "..", is relative, and sits inside
	// the coat directory. Only resolving it reveals it points elsewhere.
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"password":"hunter2"}`), 0600); err != nil {
		t.Fatal(err)
	}

	coatDir := t.TempDir()
	if err := os.Symlink(secret, filepath.Join(coatDir, "leak.json")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	resp := serveBodyFileCoat(t, coatDir, "leak.json")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a body_file symlinked outside the coat directory, got %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "hunter2") {
		t.Fatalf("the response leaked the target file's contents: %s", body)
	}
}

func TestResolveBodyFile_SymlinkWithinTheCoatDirectoryIsServed(t *testing.T) {
	// The escape check must not reject a symlink that stays inside the coat
	// directory, or the guard would be untestable from a false positive.
	coatDir := t.TempDir()
	real := filepath.Join(coatDir, "real.json")
	if err := os.WriteFile(real, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(coatDir, "alias.json")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	resp := serveBodyFileCoat(t, coatDir, "alias.json")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a symlink inside the coat directory, got %d: %s", resp.StatusCode, body)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("expected the linked file's contents, got %s", body)
	}
}

func TestResolveBodyFile_ParentTraversalIsRejected(t *testing.T) {
	// Validation rejects ".." at load time, but WithCoat/WithCoats do not
	// validate, so the runtime guard is the only thing standing between a
	// programmatically supplied coat and the file above the coat directory.
	outerDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outerDir, "secret.json"), []byte(`{"password":"hunter2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	coatDir := filepath.Join(outerDir, "coats")
	if err := os.Mkdir(coatDir, 0700); err != nil {
		t.Fatal(err)
	}

	resp := serveBodyFileCoat(t, coatDir, "../secret.json")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a body_file above the coat directory, got %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "hunter2") {
		t.Fatalf("the response leaked the target file's contents: %s", body)
	}
}

func TestResolveBodyFile_MissingFileIsReported(t *testing.T) {
	resp := serveBodyFileCoat(t, t.TempDir(), "absent.json")

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a missing body_file, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if !strings.Contains(string(body), "not found") {
		t.Fatalf("response should say the body_file was not found, got %s", body)
	}
}
