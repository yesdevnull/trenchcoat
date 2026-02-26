package coat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFile reads and parses a coat file. Format is determined by file extension.
func ParseFile(path string) (*File, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return parseYAML(path)
	case ".json":
		return parseJSON(path)
	default:
		return nil, fmt.Errorf("unrecognised coat file extension %q: expected .yaml, .yml, or .json", ext)
	}
}

func parseYAML(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading coat file %s: %w", path, err)
	}

	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing YAML coat file %s: %w", path, err)
	}
	return &f, nil
}

func parseJSON(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading coat file %s: %w", path, err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing JSON coat file %s: %w", path, err)
	}
	return &f, nil
}

// LoadDirectory reads all coat files from a directory (non-recursive).
// Files with extensions .yaml, .yml, and .json are loaded; others are skipped.
// Files are returned in lexicographic order by filename.
func LoadDirectory(dir string) ([]*File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	// Sort entries lexicographically (os.ReadDir already does this, but be explicit).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var files []*File
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		f, err := ParseFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}
