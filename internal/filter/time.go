package filter

import (
	"time"

	"github.com/north21/gonginxlog/internal/record"
)

// TimeRange keeps records with a parseable timestamp T such that
// From <= T < To. A zero From or To means that side is unbounded.
type TimeRange struct {
	From, To time.Time
}

func (t TimeRange) Match(r *record.Record) bool {
	ts, ok := r.Time()
	if !ok {
		// A time filter is active but this line's timestamp couldn't be
		// parsed - we can't confirm it's in range, so drop it.
		return false
	}
	if !t.From.IsZero() && ts.Before(t.From) {
		return false
	}
	if !t.To.IsZero() && !ts.Before(t.To) {
		return false
	}
	return true
}

// zonedTimeLayouts carry their own zone offset and are parsed as-is.
var zonedTimeLayouts = []string{
	time.RFC3339,
	"02/Jan/2006:15:04:05 -0700",
}

// localTimeLayouts have no zone offset in them. They're parsed in the
// machine's local zone (see ParseTime) rather than defaulting to UTC,
// since that's what a human typing a bare timestamp on the nginx host
// almost always means.
var localTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// ParseTime parses a --since/--until value, trying RFC3339 first and then
// nginx's own time_local layout and a couple of convenient shorthands.
//
// time.Parse defaults to UTC for any layout that has no zone offset in it,
// regardless of the process's local zone - a well-known Go gotcha. Log
// timestamps keep the offset embedded in the log line (see record.Record),
// so a bare --since/--until value parsed as UTC silently compares against
// the wrong instant unless the nginx host happens to run in UTC. We parse
// localTimeLayouts in time.Local instead, matching how someone typing a
// timestamp without a zone on the nginx host would mean it.
func ParseTime(s string) (time.Time, error) {
	var lastErr error
	for _, layout := range zonedTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	for _, layout := range localTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}
