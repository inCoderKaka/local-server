package main

import (
	"net/http"
	"sync"
	"time"
)

// inflight counts the requests a handler is serving. After a shutdown that had to close
// connections, the handlers of the closed requests are still running (net/http does not wait for
// them), and they still have to record their usage; wait lets serve hold the database and the
// workers open until they have.
//
// It uses a mutex and a channel rather than a sync.WaitGroup: net/http can still call a handler
// after Close, and a WaitGroup Add from zero during Wait is misuse.
type inflight struct {
	mu sync.Mutex
	n  int
	// idle is closed while n is 0 and replaced when n rises from 0.
	idle chan struct{}
}

func newInflight() *inflight {
	idle := make(chan struct{})
	close(idle)
	return &inflight{idle: idle}
}

// wrap counts each request h serves from its start to its return.
func (f *inflight) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if f.n == 0 {
			f.idle = make(chan struct{})
		}
		f.n++
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			f.n--
			if f.n == 0 {
				close(f.idle)
			}
			f.mu.Unlock()
		}()
		h.ServeHTTP(w, r)
	})
}

// wait returns as soon as no request is running, with drained true; or when d runs out, with
// drained false and the number of requests still running.
func (f *inflight) wait(d time.Duration) (drained bool, remaining int) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		f.mu.Lock()
		n, idle := f.n, f.idle
		f.mu.Unlock()
		if n == 0 {
			return true, 0
		}
		select {
		case <-idle:
			// Re-check: a new request may have started since.
		case <-timer.C:
			f.mu.Lock()
			n = f.n
			f.mu.Unlock()
			return n == 0, n
		}
	}
}
