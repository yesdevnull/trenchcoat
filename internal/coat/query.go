package coat

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML implements the yaml.Unmarshaler interface for QueryField.
// It handles both string and map[string]string forms.
func (q *QueryField) UnmarshalYAML(value *yaml.Node) error {
	// Try as a string first.
	if value.Kind == yaml.ScalarNode {
		q.Raw = value.Value
		return nil
	}

	// Try as a map.
	if value.Kind == yaml.MappingNode {
		m := make(map[string]string)
		if err := value.Decode(&m); err != nil {
			return fmt.Errorf("query: expected string or map of string pairs: %w", err)
		}
		q.Map = m
		return nil
	}

	return fmt.Errorf("query: expected string or map, got %v", value.Kind)
}

// UnmarshalJSON implements the json.Unmarshaler interface for QueryField.
// It handles both string and map[string]string forms.
func (q *QueryField) UnmarshalJSON(data []byte) error {
	// Try as a string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		q.Raw = s
		return nil
	}

	// Try as a map.
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		q.Map = m
		return nil
	}

	return fmt.Errorf("query: expected string or object")
}
