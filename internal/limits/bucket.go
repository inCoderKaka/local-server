package limits

import (
	"sync"
	"time"
)

// buckets are token buckets by access key id. A bucket starts full, holds at most capacity
// requests, and refills evenly so an empty one is full again after window.
type buckets struct {
	mu        sync.Mutex
	m         map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newBuckets() *buckets { return &buckets{m: map[string]*bucket{}} }

// take uses one request from key's bucket. If the bucket is empty it returns false and the
// time until one request is available.
func (bs *buckets) take(key string, capacity int64, window time.Duration, now time.Time) (bool, time.Duration) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	// A bucket idle for a whole window is full again, the same as no bucket, so it can go.
	if now.Sub(bs.lastSweep) >= window {
		for k, b := range bs.m {
			if now.Sub(b.at) >= window {
				delete(bs.m, k)
			}
		}
		bs.lastSweep = now
	}
	c := float64(capacity)
	perNano := c / float64(window)
	b, ok := bs.m[key]
	if !ok {
		b = &bucket{tokens: c, at: now}
		bs.m[key] = b
	}
	if el := now.Sub(b.at); el > 0 {
		b.tokens += float64(el) * perNano
		b.at = now
	}
	if b.tokens > c {
		b.tokens = c
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / perNano)
	return false, retryAfterAtLeast(wait+1, time.Millisecond)
}
