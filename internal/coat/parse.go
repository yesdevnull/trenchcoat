package coat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFile reads and parses a coat file. Format is determined by file extension.
func ParseFile(path string) (*File, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return parseFileWith(path, "YAML", yaml.Unmarshal)
	case ".json":
		return parseFileWith(path, "JSON", json.Unmarshal)
	default:
		return nil, fmt.Errorf("unrecognised coat file extension %q: expected .yaml, .yml, or .json", ext)
	}
}

func parseFileWith(path, format string, unmarshal func([]byte, any) error) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading coat file %s: %w", path, err)
	}

	var f File
	if err := unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s coat file %s: %w", format, path, err)
	}
	return &f, nil
}
