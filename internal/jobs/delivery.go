package jobs

import (
	"errors"
	"io"
	"net/http"
)

// Delivery wraps a response writer and tells afterwards whether the whole response reached the
// connection. A job result is deleted only when it did (req_001 D-85, refining D-40), so a dropped
// connection leaves the result fetchable until its time-to-live (req_001 D-26).
//
// "Whole" is what net/http can see: every Write was accepted in full, the status was a success,
// and a final flush of the buffered bytes to the connection returned no error. It cannot see the
// client's acknowledgement; a connection that dies after the flush has already accepted the bytes.
type Delivery struct {
	http.ResponseWriter
	status int
	err    error
}

// NewDelivery wraps w.
func NewDelivery(w http.ResponseWriter) *Delivery { return &Delivery{ResponseWriter: w} }

// Unwrap lets http.ResponseController reach the inner writer to flush it.
func (d *Delivery) Unwrap() http.ResponseWriter { return d.ResponseWriter }

// WriteHeader records the first status.
func (d *Delivery) WriteHeader(code int) {
	if d.status == 0 {
		d.status = code
	}
	d.ResponseWriter.WriteHeader(code)
}

// Write records a failed or short write.
func (d *Delivery) Write(p []byte) (int, error) {
	if d.status == 0 {
		d.status = http.StatusOK
	}
	n, err := d.ResponseWriter.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil && d.err == nil {
		d.err = err
	}
	return n, err
}

// Complete reports whether the response was written whole. Call it after the handler returned. It
// flushes the writer first, so bytes still in net/http's buffer have reached the connection; a
// writer that cannot flush is judged by its writes alone.
func (d *Delivery) Complete() bool {
	if d.err != nil || d.status < 200 || d.status > 299 {
		return false
	}
	if err := http.NewResponseController(d.ResponseWriter).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		d.err = err
		return false
	}
	return true
}
