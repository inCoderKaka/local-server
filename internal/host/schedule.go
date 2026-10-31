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

// Schedule is a daily quiet-hours window. The zero value is off.
type Schedule struct {
	Enabled bool
	// Location is the zone the window is read in; nil means UTC.
	Location *time.Location
	// Start and End are offsets from local midnight. The window is [Start, End); when End is
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
