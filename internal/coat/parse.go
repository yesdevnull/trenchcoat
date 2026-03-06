package coat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// varPattern matches ${VAR_NAME} and ${VAR_NAME:-default} syntax.
// Default values cannot contain closing braces.
var varPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

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

	// Substitute ${VAR:-default} variables from environment before parsing.
	data = substituteVars(data)

	var f File
	if err := unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s coat file %s: %w", format, path, err)
	}
	return &f, nil
}

// substituteVars replaces ${VAR_NAME} and ${VAR_NAME:-default} patterns with
// environment variable values. If a variable is unset and has no default, the
// pattern is left unchanged.
func substituteVars(data []byte) []byte {
	return varPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := varPattern.FindSubmatch(match)
		name := string(groups[1])
		val, ok := os.LookupEnv(name)
		if ok {
			return []byte(val)
		}
		// Check if a default was provided (group 2).
		if len(groups) > 2 && groups[2] != nil {
			return groups[2]
		}
		// No env var set, no default — leave as-is.
		return match
	})
}
