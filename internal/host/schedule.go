package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the quiet-hours settings (D-06, D-38, D-54). All are owned by the host.
const (
	KeyQuietEnabled  = "host.quiet_hours.enabled"
	KeyQuietTimeZone = "host.quiet_hours.time_zone"
	KeyQuietStart    = "host.quiet_hours.start"
	KeyQuietEnd      = "host.quiet_hours.end"
)

// ErrInvalidSchedule is returned when the quiet-hours settings do not hold a usable schedule.
var ErrInvalidSchedule = errors.New("host: invalid quiet-hours schedule")

// Defs are the quiet-hours settings; register them with settings.Store.Register. Quiet hours are
// off until the host turns them on.
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeyQuietEnabled, Kind: settings.Bool, Default: "false", Owner: settings.Host,
			Description: "Whether the host stops serving every day during the quiet-hours window."},
		{Key: KeyQuietTimeZone, Kind: settings.String, Default: "Local", Owner: settings.Host,
			Description: "The time zone of the quiet-hours window: an IANA name such as Europe/Berlin, or Local for this machine's zone."},
		{Key: KeyQuietStart, Kind: settings.String, Default: "22:00", Owner: settings.Host,
			Description: "When quiet hours begin each day, as HH:MM (24 hours) in the quiet-hours time zone."},
		{Key: KeyQuietEnd, Kind: settings.String, Default: "07:00", Owner: settings.Host,
			Description: "When quiet hours end each day, as HH:MM (24 hours) in the quiet-hours time zone. An end before the start means the window crosses midnight."},
	}
}

// RegisterValidators makes the settings store refuse a bad quiet-hours start, end or time zone
// at write time instead of when LoadSchedule reads it. Register Defs first: a validator needs
// its key to exist.
func RegisterValidators(st *settings.Store) error {
	clock := func(_ context.Context, v string) error {
		if _, err := time.Parse("15:04", v); err != nil {
			return errors.New("must be HH:MM")
		}
		return nil
	}
	if err := st.SetValidator(KeyQuietStart, clock); err != nil {
		return err
	}
	if err := st.SetValidator(KeyQuietEnd, clock); err != nil {
		return err
	}
	return st.SetValidator(KeyQuietTimeZone, func(_ context.Context, v string) error {
		if v == "" {
			return errors.New("must not be empty")
		}
		if _, err := time.LoadLocation(v); err != nil {
			return errors.New("not a known time zone")
		}
		return nil
	})
}

// Schedule is a daily quiet-hours window. The zero value is off.
type Schedule struct {
	Enabled bool
	// Location is the zone the window is read in; nil means UTC.
	Location *time.Location
	// Start and End are wall-clock times of day (as offsets from 00:00) in Location. The window is [Start, End); when End is
	// before Start it crosses midnight; when they are equal the window is empty.
	Start, End time.Duration
}

// Quiet reports whether t falls inside the window.
func (s Schedule) Quiet(t time.Time) bool {
	if !s.Enabled || s.Start == s.End {
		return false
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	h, m, sec := t.In(loc).Clock()
	at := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second +
		time.Duration(t.Nanosecond())
	if s.Start < s.End {
		return at >= s.Start && at < s.End
	}
	return at >= s.Start || at < s.End
}

// ParseClock parses "HH:MM" into an offset from midnight.
func ParseClock(v string) (time.Duration, error) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not HH:MM", ErrInvalidSchedule, v)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// LoadSchedule reads the quiet-hours settings.
func LoadSchedule(ctx context.Context, st *settings.Store) (Schedule, error) {
	on, err := st.Bool(ctx, KeyQuietEnabled, "")
	if err != nil {
		return Schedule{}, err
	}
	if !on {
		return Schedule{}, nil
	}
	zone, err := st.String(ctx, KeyQuietTimeZone, "")
	if err != nil {
		return Schedule{}, err
	}
	if zone == "" {
		return Schedule{}, fmt.Errorf("%w: time zone is empty; use an IANA name or Local", ErrInvalidSchedule)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Schedule{}, fmt.Errorf("%w: time zone %q: %v", ErrInvalidSchedule, zone, err)
	}
	startText, err := st.String(ctx, KeyQuietStart, "")
	if err != nil {
		return Schedule{}, err
	}
	endText, err := st.String(ctx, KeyQuietEnd, "")
	if err != nil {
		return Schedule{}, err
	}
	start, err := ParseClock(startText)
	if err != nil {
		return Schedule{}, err
	}
	end, err := ParseClock(endText)
	if err != nil {
		return Schedule{}, err
	}
	return Schedule{Enabled: true, Location: loc, Start: start, End: end}, nil
}

// NextEnd returns when the quiet window that contains t ends (req_001 D-05, D-06). It is only
// meaningful while Quiet(t) is true; the zero Time is returned otherwise.
func (s Schedule) NextEnd(t time.Time) time.Time {
	if !s.Quiet(t) {
		return time.Time{}
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	end := wallTime(y, m, d, s.End, loc)
	if !end.After(t) {
		end = wallTime(y, m, d+1, s.End, loc)
	}
	return end
}

// wallTime returns the instant the clock in loc shows off past midnight on y-m-d. When that
// wall time falls in a daylight-saving gap it does not exist, and the window ends at the
// transition, the first instant the clock shows a later time.
func wallTime(y int, m time.Month, d int, off time.Duration, loc *time.Location) time.Time {
	hh, mm := int(off/time.Hour), int(off%time.Hour/time.Minute)
	c := time.Date(y, m, d, hh, mm, 0, 0, loc)
	want := time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
	cy, cm, cd := c.Date()
	ch, cmin, _ := c.Clock()
	got := time.Date(cy, cm, cd, ch, cmin, 0, 0, time.UTC)
	switch {
	case got.After(want): // normalised past the gap
		start, _ := c.ZoneBounds()
		return start
	case got.Before(want): // normalised before the gap
		_, end := c.ZoneBounds()
		return end
	}
	return c
}
