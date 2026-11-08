// Package gzipcodec is the default snapshot.Compressor adapter: the standard library's gzip
// (req_001 D-103).
package gzipcodec

import (
	"compress/gzip"
	"io"
)

// Codec compresses with gzip.
type Codec struct {
	// Level is a compress/gzip level; the zero value means gzip.DefaultCompression.
	Level int
}

// Name returns "gzip".
func (Codec) Name() string { return "gzip" }

// NewWriter implements snapshot.Compressor.
func (c Codec) NewWriter(w io.Writer) (io.WriteCloser, error) {
	level := c.Level
	if level == 0 {
		level = gzip.DefaultCompression
	}
	return gzip.NewWriterLevel(w, level)
}

// NewReader implements snapshot.Compressor.
func (Codec) NewReader(r io.Reader) (io.ReadCloser, error) { return gzip.NewReader(r) }
