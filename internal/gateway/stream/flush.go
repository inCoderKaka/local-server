package stream

import (
	"net/http"
	"strings"
)

// NewFlushWriter wraps w so that a text/event-stream response is flushed after its headers and
// after every Write. ogen's generated encoder copies a stream with io.Copy and never flushes
// (PLT-B-004), so without this the events would sit in net/http's buffer until it fills or the
// stream ends. Every other response passes through untouched. The wrapper implements Unwrap, so
// http.ResponseController still reaches the underlying writer.
func NewFlushWriter(w http.ResponseWriter) http.ResponseWriter {
	return &flushWriter{ResponseWriter: w}
}

type flushWriter struct {
	http.ResponseWriter
	decided, sse bool
}

func (f *flushWriter) Unwrap() http.ResponseWriter { return f.ResponseWriter }

func (f *flushWriter) decide() {
	if f.decided {
		return
	}
	f.decided = true
	f.sse = strings.HasPrefix(f.Header().Get("Content-Type"), "text/event-stream")
	if f.sse && f.Header().Get("Cache-Control") == "" {
		f.Header().Set("Cache-Control", "no-cache")
	}
}

func (f *flushWriter) flush() {
	if f.sse {
		_ = http.NewResponseController(f.ResponseWriter).Flush()
	}
}

func (f *flushWriter) WriteHeader(code int) {
	f.decide()
	f.ResponseWriter.WriteHeader(code)
	f.flush()
}

func (f *flushWriter) Write(p []byte) (int, error) {
	f.decide()
	n, err := f.ResponseWriter.Write(p)
	if n > 0 {
		f.flush()
	}
	return n, err
}
