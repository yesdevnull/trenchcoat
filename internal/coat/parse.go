package coat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		return parseFileWith(path, "YAML", unmarshalStrictYAML)
	case ".json":
		return parseFileWith(path, "JSON", unmarshalStrictJSON)
	default:
		return nil, fmt.Errorf("unrecognised coat file extension %q: expected .yaml, .yml, or .json", ext)
	}
}

// unmarshalStrictYAML decodes YAML, rejecting keys that do not correspond to a
// field. coatfile.schema.json sets additionalProperties: false throughout, and
// silently discarding unknown keys contradicts that in the way that costs most:
// a misspelt 'mehtod' leaves the coat defaulting to GET, so the POSTs it was
// written for 404 while unintended GETs match, and `trenchcoat validate` calls
// the file clean.
//
// It also rejects anything after the first document. yaml.Decoder.Decode reads
// one document and stops, so without this check every coat after a '---'
// separator silently does not exist, and syntactically broken content after a
// document is accepted without a word. A null document is tolerated so the
// markers a single-document file may carry -- a trailing '---' or '...' -- keep
// working.
func unmarshalStrictYAML(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		// An empty document is not an error: it yields a File with no coats,
		// which the existing behaviour allowed and validation reports on.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	for {
		var extra any
		err := dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if extra != nil {
			return fmt.Errorf("unexpected content after the YAML document")
		}
	}
}

// unmarshalStrictJSON decodes JSON, rejecting unknown keys for the same reason.
//
// It also rejects anything after the document. json.Unmarshal required the
// whole input to be a single value; json.Decoder.Decode reads one value and
// stops, so without this check a second appended object -- or any trailing
// garbage -- loads silently and the coats after it simply do not exist.
func unmarshalStrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	if dec.More() {
		return fmt.Errorf("unexpected content after the JSON document")
	}
	return nil
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

	// Top-level keys outside the schema are accepted only as "x-" extensions.
	// That exists for one idiom: a key whose only job is to hold a YAML anchor
	// that coats below merge in. Everything else is a typo, and letting a typo
	// through is what strict decoding is for.
	//
	// Only YAML ever reaches this loop. Extensions is json:"-", so a JSON x- key
	// is an unknown field to the JSON decoder and is refused there -- which is
	// the intended scope, since JSON has no anchors and so no use for the idiom.
	// The format is still named in the message because parseFileWith is shared.
	for key := range f.Extensions {
		if !strings.HasPrefix(key, "x-") {
			return nil, fmt.Errorf("parsing %s coat file %s: unknown top-level field %q (only \"x-\" prefixed extension keys are allowed)", format, path, key)
		}
	}

	return &f, nil
}

// substituteVars replaces ${VAR_NAME} and ${VAR_NAME:-default} patterns with
// environment variable values. If a variable is unset and has no default, the
// pattern is left unchanged. The :- syntax uses shell semantics: the default
// is used when the variable is unset OR empty.
func substituteVars(data []byte) []byte {
	return varPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := varPattern.FindSubmatch(match)
		name := string(groups[1])
		val, ok := os.LookupEnv(name)
		hasDefault := len(groups) > 2 && groups[2] != nil

		// Shell :- semantics: use the env value if the variable is set.
		// With :- syntax, an empty value falls through to the default.
		// Without :- syntax, an empty value is returned as-is.
		if ok {
			if !hasDefault || val != "" {
				return []byte(val)
			}
		}
		// Use the default when provided (shell :- semantics: unset or empty).
		if hasDefault {
			return groups[2]
		}
		// No env var set, no default — leave as-is.
		return match
	})
}
