package limits

import (
	"testing"

	"github.com/inCoderKaka/local-server/internal/usage"
)

func TestAdmittedSeedRetry(t *testing.T) {
	a := newAdmitted()
	k := admitKey{by: usage.ByAccessKey, id: "k", start: 1000}
	if !a.needsSeed(k) {
		t.Fatal("new counter: needsSeed = false")
	}
	a.add(k, 0, false)
	if got := a.peek(k); got != 1 || !a.needsSeed(k) {
		t.Fatalf("after failed seed: peek = %d, needsSeed = %v, want 1, true", got, a.needsSeed(k))
	}
	a.add(k, 5, true)
	if got := a.peek(k); got != 6 || a.needsSeed(k) {
		t.Fatalf("after seed: peek = %d, needsSeed = %v, want 6, false", got, a.needsSeed(k))
	}
	a.add(k, 99, true)
	if got := a.peek(k); got != 7 {
		t.Fatalf("seed after seeded: peek = %d, want 7", got)
	}
	a.add(admitKey{by: usage.ByAccessKey, id: "k", start: 2000}, 0, true)
	if got := a.peek(k); got != 0 {
		t.Fatalf("old window: peek = %d, want 0 (swept)", got)
	}
}
