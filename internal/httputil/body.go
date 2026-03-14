// Package httputil provides shared HTTP utility functions.
package httputil

import (
	"bytes"
	"io"
)

// ReconstitutedBody returns a new ReadCloser that first replays the captured
// bytes, then continues reading from the remaining original body. Close
// delegates to the original body's Close method.
func ReconstitutedBody(captured []byte, orig io.ReadCloser) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(captured), orig),
		Closer: orig,
	}
}
