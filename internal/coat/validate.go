package coat

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError represents a single validation error for a coat.
type ValidationError struct {
	CoatIndex int
	CoatName  string
	Message   string
}

func (e *ValidationError) Error() string {
	name := e.CoatName
	if name == "" {
		name = fmt.Sprintf("coat[%d]", e.CoatIndex)
	}
	return fmt.Sprintf("%s: %s", name, e.Message)
}

// Validate checks a parsed coat file for schema correctness.
// Returns a slice of validation errors (empty if valid).
func Validate(f *File) []*ValidationError {
	var errs []*ValidationError

	for i, c := range f.Coats {
		coatErrs := validateCoat(i, c)
		errs = append(errs, coatErrs...)
	}

	return errs
}

func validateCoat(index int, c Coat) []*ValidationError {
	var errs []*ValidationError

	mkErr := func(msg string) *ValidationError {
		return &ValidationError{
			CoatIndex: index,
			CoatName:  c.Name,
			Message:   msg,
		}
	}

	// URI is mandatory.
	if c.Request.URI == "" {
		errs = append(errs, mkErr("request.uri is required"))
	}

	// Validate regex URI syntax.
	if strings.HasPrefix(c.Request.URI, "~/") {
		pattern := strings.TrimPrefix(c.Request.URI, "~")
		if _, err := regexp.Compile("^" + pattern + "$"); err != nil {
			errs = append(errs, mkErr(fmt.Sprintf("request.uri has invalid regex %q: %v", c.Request.URI, err)))
		}
	}

	// Must have either response or responses, not both.
	hasResponse := c.Response != nil
	hasResponses := len(c.Responses) > 0

	if hasResponse && hasResponses {
		errs = append(errs, mkErr("coat must have either 'response' or 'responses', not both"))
	}
	if !hasResponse && !hasResponses {
		errs = append(errs, mkErr("coat must have either 'response' or 'responses'"))
	}

	// Validate body/body_file mutual exclusivity in singular response.
	if hasResponse {
		if c.Response.Body != "" && c.Response.BodyFile != "" {
			errs = append(errs, mkErr("response: 'body' and 'body_file' are mutually exclusive"))
		}
	}

	// Validate body/body_file mutual exclusivity in plural responses.
	if hasResponses {
		for j, r := range c.Responses {
			if r.Body != "" && r.BodyFile != "" {
				errs = append(errs, mkErr(fmt.Sprintf("responses[%d]: 'body' and 'body_file' are mutually exclusive", j)))
			}
		}
	}

	// Sequence is only valid with responses (plural).
	if c.Sequence != "" {
		if !hasResponses {
			errs = append(errs, mkErr("'sequence' is only valid with 'responses' (plural)"))
		}
		if c.Sequence != "cycle" && c.Sequence != "once" {
			errs = append(errs, mkErr(fmt.Sprintf("'sequence' must be 'cycle' or 'once', got %q", c.Sequence)))
		}
	}

	return errs
}
