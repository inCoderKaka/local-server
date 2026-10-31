package limits

import (
	"context"
	"io"
	"net/http"
)

// LimitBody enforces the body size setting on r. A request whose Content-Length is over the
// limit fails at once; otherwise r.Body is replaced by a reader that fails with a
// *TooLargeError as soon as more than the limit has been read, so a decoder or io.ReadAll
// sees the typed error (use errors.Is(err, ErrTooLarge) or errors.As). With the limit turned
// off it does nothing.
func (l *Limiter) LimitBody(ctx context.Context, r *http.Request) error {
	on, err := l.cfg.Settings.Bool(ctx, KeyBodyLimitEnabled, "")
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	max, err := l.cfg.Settings.Int(ctx, KeyMaxBodyBytes, "")
	if err != nil {
		return err
	}
	if max < 0 {
		max = 0
	}
	if r.ContentLength > max {
		return &TooLargeError{Limit: max}
	}
	if r.Body != nil {
		r.Body = &limitedBody{rc: r.Body, max: max}
	}
	return nil
}

type limitedBody struct {
	rc   io.ReadCloser
	max  int64
	read int64
	err  error
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if room := b.max - b.read + 1; int64(len(p)) > room {
		p = p[:room] // read at most one byte past the limit to tell "exactly max" from "over"
	}
	n, err := b.rc.Read(p)
	b.read += int64(n)
	if b.read > b.max {
		n -= int(b.read - b.max)
		b.err = &TooLargeError{Limit: b.max}
		return n, b.err
	}
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }
