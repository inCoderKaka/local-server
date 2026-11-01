package host_test

import (
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/host"
)

func TestScheduleNextEnd(t *testing.T) {
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, time.UTC) }
	s := host.Schedule{Enabled: true, Start: 22 * time.Hour, End: 6 * time.Hour} // crosses midnight
	for _, c := range []struct{ now, want time.Time }{
		{at(1, 23, 0), at(2, 6, 0)}, // before midnight: ends tomorrow
		{at(2, 2, 0), at(2, 6, 0)},  // after midnight: ends today
	} {
		if got := s.NextEnd(c.now); !got.Equal(c.want) {
			t.Errorf("NextEnd(%v) = %v, want %v", c.now, got, c.want)
		}
	}
	if got := s.NextEnd(at(1, 12, 0)); !got.IsZero() {
		t.Errorf("outside the window NextEnd = %v, want zero", got)
	}
	day := host.Schedule{Enabled: true, Start: 9 * time.Hour, End: 17 * time.Hour}
	if got := day.NextEnd(at(1, 10, 0)); !got.Equal(at(1, 17, 0)) {
		t.Errorf("same-day NextEnd = %v", got)
	}
}
