package host_test

import (
	"testing"
	"time"
	_ "time/tzdata" // so LoadLocation behaves the same on every CI OS

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

func TestScheduleNextEndDST(t *testing.T) {
	zone := func(name string) *time.Location {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		return loc
	}
	berlin, santiago := zone("Europe/Berlin"), zone("America/Santiago")
	at := func(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, loc)
	}
	for _, c := range []struct {
		name       string
		loc        *time.Location
		start, end time.Duration
		now, want  time.Time
	}{
		{"berlin spring forward, after the gap", berlin, 22 * time.Hour, 7 * time.Hour,
			at(berlin, 2026, 3, 29, 3, 0), at(berlin, 2026, 3, 29, 7, 0)},
		{"berlin spring forward, night before", berlin, 22 * time.Hour, 7 * time.Hour,
			at(berlin, 2026, 3, 28, 23, 0), at(berlin, 2026, 3, 29, 7, 0)},
		{"berlin fall back", berlin, 22 * time.Hour, 7 * time.Hour,
			at(berlin, 2026, 10, 25, 1, 0), at(berlin, 2026, 10, 25, 7, 0)},
		{"berlin end falls in the gap", berlin, 22 * time.Hour, 2*time.Hour + 30*time.Minute,
			at(berlin, 2026, 3, 29, 1, 30), at(berlin, 2026, 3, 29, 3, 0)},
		{"santiago DST starts at midnight", santiago, 22 * time.Hour, 0,
			at(santiago, 2026, 9, 5, 23, 30), at(santiago, 2026, 9, 6, 1, 0)},
		{"utc midnight end", time.UTC, 22 * time.Hour, 0,
			at(time.UTC, 2026, 11, 1, 23, 0), at(time.UTC, 2026, 11, 2, 0, 0)},
	} {
		s := host.Schedule{Enabled: true, Location: c.loc, Start: c.start, End: c.end}
		if !s.Quiet(c.now) {
			t.Errorf("%s: Quiet(%v) = false, want true", c.name, c.now)
			continue
		}
		got := s.NextEnd(c.now)
		if !got.Equal(c.want) {
			t.Errorf("%s: NextEnd(%v) = %v, want %v", c.name, c.now, got, c.want)
		}
		if !got.After(c.now) {
			t.Errorf("%s: NextEnd %v is not after %v", c.name, got, c.now)
		}
		if !s.Quiet(got.Add(-time.Second)) {
			t.Errorf("%s: not quiet just before %v", c.name, got)
		}
		if s.Quiet(got) {
			t.Errorf("%s: still quiet at %v", c.name, got)
		}
	}
}
