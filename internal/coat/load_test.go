package coat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPaths_SingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.yaml")
	writeFile(t, f, `
coats:
  - name: "single"
    request:
      uri: "/test"
    response:
      code: 200
      body: "ok"
`)

	loaded, errs := LoadPaths([]string{f})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 coat, got %d", len(loaded))
	}
	if loaded[0].Coat.Name != "single" {
		t.Fatalf("expected name 'single', got %q", loaded[0].Coat.Name)
	}
	if loaded[0].FilePath != f {
		t.Fatalf("expected FilePath %q, got %q", f, loaded[0].FilePath)
	}
}

func TestLoadPaths_Directory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `
coats:
  - name: "alpha"
    request:
      uri: "/alpha"
    response:
      code: 200
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
coats:
  - name: "beta"
    request:
      uri: "/beta"
    response:
      code: 201
`)
	// Non-coat file should be ignored.
	writeFile(t, filepath.Join(dir, "readme.txt"), "not a coat")

	loaded, errs := LoadPaths([]string{dir})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 coats, got %d", len(loaded))
	}
	// Should be in lexicographic order.
	if loaded[0].Coat.Name != "alpha" {
		t.Fatalf("expected first coat 'alpha', got %q", loaded[0].Coat.Name)
	}
	if loaded[1].Coat.Name != "beta" {
		t.Fatalf("expected second coat 'beta', got %q", loaded[1].Coat.Name)
	}
}

func TestLoadPaths_MixedFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	singleFile := filepath.Join(dir, "single.yaml")
	writeFile(t, singleFile, `
coats:
  - name: "from-file"
    request:
      uri: "/file"
    response:
      code: 200
`)
	writeFile(t, filepath.Join(subdir, "dir-coat.yaml"), `
coats:
  - name: "from-dir"
    request:
      uri: "/dir"
    response:
      code: 201
`)

	loaded, errs := LoadPaths([]string{singleFile, subdir})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 coats, got %d", len(loaded))
	}
}

func TestLoadPaths_NonExistentPath(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	loaded, errs := LoadPaths([]string{nonExistent})
	if len(errs) == 0 {
		t.Fatal("expected errors for non-existent path")
	}
	if !strings.Contains(errs[0].Error(), "cannot access") {
		t.Fatalf("expected 'cannot access' error, got: %v", errs[0])
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 coats, got %d", len(loaded))
	}
}

func TestLoadPaths_ValidationErrors(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "invalid.yaml")
	writeFile(t, f, `
coats:
  - name: "no-uri"
    response:
      code: 200
`)

	loaded, errs := LoadPaths([]string{f})
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	if !strings.Contains(errs[0].Error(), "request.uri is required") {
		t.Fatalf("expected uri required error, got: %v", errs[0])
	}
	// The coat should still be loaded (validation errors are separate).
	if len(loaded) != 1 {
		t.Fatalf("expected 1 coat loaded despite errors, got %d", len(loaded))
	}
}

func TestLoadPaths_Empty(t *testing.T) {
	loaded, errs := LoadPaths(nil)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 coats, got %d", len(loaded))
	}
}

func TestLoadPaths_SubdirSkipped(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(subdir, "nested.yaml"), `
coats:
  - name: "nested"
    request:
      uri: "/nested"
    response:
      code: 200
`)
	writeFile(t, filepath.Join(dir, "top.yaml"), `
coats:
  - name: "top"
    request:
      uri: "/top"
    response:
      code: 200
`)

	// Loading the top dir should not recurse into subdir.
	loaded, errs := LoadPaths([]string{dir})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 coat (non-recursive), got %d", len(loaded))
	}
	if loaded[0].Coat.Name != "top" {
		t.Fatalf("expected 'top', got %q", loaded[0].Coat.Name)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}
