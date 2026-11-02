package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// wait returns true once a blocked handler returns, and false when the timeout runs out while a
// handler is still blocked.
func TestInflightWait(t *testing.T) {
	f := newInflight()
	entered := make(chan struct{})
	release := make(chan struct{})
	h := f.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		entered <- struct{}{}
		<-release
	}))
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	<-entered

	if drained, remaining := f.wait(20 * time.Millisecond); drained || remaining != 1 {
		t.Fatalf("wait with a handler blocked = (%v, %d), want (false, 1)", drained, remaining)
	}

	result := make(chan bool, 1)
	go func() {
		drained, _ := f.wait(5 * time.Second)
		result <- drained
	}()
	close(release)
	<-done
	select {
	case drained := <-result:
		if !drained {
			t.Error("wait = false after the handler returned, want true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after the handler returned")
	}

	// Idle again.
	if drained, remaining := f.wait(time.Second); !drained || remaining != 0 {
		t.Errorf("wait when idle = (%v, %d), want (true, 0)", drained, remaining)
	}
}

// The count restarts from zero after the handler has been idle.
func TestInflightCountsAgainAfterIdle(t *testing.T) {
	f := newInflight()
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	h := f.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		entered <- struct{}{}
		<-block
	}))
	run := func() chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		}()
		<-entered
		return done
	}
	first := run()
	block <- struct{}{}
	<-first
	second := run()
	if drained, remaining := f.wait(20 * time.Millisecond); drained || remaining != 1 {
		t.Errorf("wait during the second request = (%v, %d), want (false, 1)", drained, remaining)
	}
	block <- struct{}{}
	<-second
	if drained, _ := f.wait(time.Second); !drained {
		t.Error("wait = false after both requests returned")
	}
}
