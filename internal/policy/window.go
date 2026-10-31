package policy

import (
	"errors"
	"regexp"
	"time"
)

// window is a daily time window [start, end) in a rule's time zone, as offsets from local
// midnight. When end is before start the window crosses midnight. start == end is refused.
type window struct {
	start, end time.Duration
}

var clockRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// parseClock reads "HH:MM" (24 hours, two digits each) into an offset from midnight.
func parseClock(s string) (time.Duration, bool) {
	m := clockRE.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	h := int(m[1][0]-'0')*10 + int(m[1][1]-'0')
	mi := int(m[2][0]-'0')*10 + int(m[2][1]-'0')
	return time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute, true
}

func parseWindow(w windowV1) (window, error) {
	if w.Start == nil || w.End == nil {
		return window{}, errors.New("start and end are both required")
	}
	start, ok := parseClock(*w.Start)
	if !ok {
		return window{}, errors.New("start is not HH:MM")
	}
	end, ok := parseClock(*w.End)
	if !ok {
		return window{}, errors.New("end is not HH:MM")
	}
	if start == end {
		return window{}, errors.New("start and end are equal; leave windows out for no time restriction")
	}
	return window{start: start, end: end}, nil
}

// zoneRE bounds what is handed to time.LoadLocation: IANA names such as Europe/Berlin or
// America/Argentina/Buenos_Aires, Etc/GMT+5, UTC, and Local (the host machine's zone).
var zoneRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9][A-Za-z0-9_+-]*){0,2}$`)

func loadZone(name string) (*time.Location, error) {
	if len(name) > 64 || !zoneRE.MatchString(name) {
		return nil, ruleErr("time_zone %q is not a time zone name", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ruleErr("time_zone %q is not known on this host", name)
	}
	return loc, nil
}

// clockOf returns t's wall-clock offset from midnight in loc.
func clockOf(t time.Time, loc *time.Location) time.Duration {
	h, m, s := t.In(loc).Clock()
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second +
		time.Duration(t.Nanosecond())
}

func (w window) contains(at time.Duration) bool {
	if w.start < w.end {
		return at >= w.start && at < w.end
	}
	return at >= w.start || at < w.end
}

// inWindows reports whether t falls inside any of the rule's windows.
func (r *rule) inWindows(t time.Time) bool {
	at := clockOf(t, r.loc)
	for _, w := range r.windows {
		if w.contains(at) {
			return true
		}
	}
	return false
}

// nextOpen returns the earliest start of one of the rule's windows after t, read as wall-clock
// time in the rule's zone (a start that does not exist on a daylight-saving day moves as
// time.Date moves it).
func (r *rule) nextOpen(t time.Time) time.Time {
	local := t.In(r.loc)
	y, mo, d := local.Date()
	var best time.Time
	for _, w := range r.windows {
		h, m := int(w.start/time.Hour), int(w.start%time.Hour/time.Minute)
		c := time.Date(y, mo, d, h, m, 0, 0, r.loc)
		for i := 0; !c.After(t) && i < 3; i++ {
			d2 := local.AddDate(0, 0, i+1)
			c = time.Date(d2.Year(), d2.Month(), d2.Day(), h, m, 0, 0, r.loc)
		}
		if best.IsZero() || c.Before(best) {
			best = c
		}
	}
	return best
}
