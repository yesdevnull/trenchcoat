// Package coat provides types and parsing for Trenchcoat coat files.
// A coat is an individual request/response mock definition.
package coat

// File represents the top-level structure of a coat file.
type File struct {
	Coats []Coat `yaml:"coats" json:"coats"`
}

// Coat is an individual request/response mock definition.
type Coat struct {
	Name      string     `yaml:"name,omitempty" json:"name,omitempty"`
	Request   Request    `yaml:"request" json:"request"`
	Response  *Response  `yaml:"response,omitempty" json:"response,omitempty"`
	Responses []Response `yaml:"responses,omitempty" json:"responses,omitempty"`
	Sequence  string     `yaml:"sequence,omitempty" json:"sequence,omitempty"`
}

// Request defines the matching criteria for an incoming HTTP request.
type Request struct {
	Method    string      `yaml:"method,omitempty" json:"method,omitempty"`
	URI       string      `yaml:"uri" json:"uri"`
	Headers   StringMap   `yaml:"headers,omitempty" json:"headers,omitempty"`
	Query     *QueryField `yaml:"query,omitempty" json:"query,omitempty"`
	Body      *string     `yaml:"body,omitempty" json:"body,omitempty"`
	BodyMatch string      `yaml:"body_match,omitempty" json:"body_match,omitempty"`
}

// Response defines the mock response to return.
type Response struct {
	Code          int       `yaml:"code,omitempty" json:"code,omitempty"`
	Headers       StringMap `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body          string    `yaml:"body,omitempty" json:"body,omitempty"`
	BodyFile      string    `yaml:"body_file,omitempty" json:"body_file,omitempty"`
	DelayMs       int       `yaml:"delay_ms,omitempty" json:"delay_ms,omitempty"`
	DelayJitterMs int       `yaml:"delay_jitter_ms,omitempty" json:"delay_jitter_ms,omitempty"`
}

// StringMap is a map[string]string used for headers.
type StringMap = map[string]string

// StringPtr returns a pointer to s. It is a convenience helper for constructing
// Request literals with a body constraint.
func StringPtr(s string) *string { return &s }

// QueryField represents the query field which can be either a string or a map.
type QueryField struct {
	Raw string
	Map map[string]string
}
