package directory

import (
	"context"
	"testing"
)

func TestNoopKnowsNoHosts(t *testing.T) {
	got, err := Noop{}.Discover(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("Discover() = %v, %v; want no entries and no error", got, err)
	}
}
