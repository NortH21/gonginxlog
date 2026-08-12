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

// nginxTimeLayouts mirrors record.Record's own list, used to parse
// --since/--until values given in nginx's own time_local format.
var nginxTimeLayouts = []string{
	time.RFC3339,
	"02/Jan/2006:15:04:05 -0700",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// ParseTime parses a --since/--until value, trying RFC3339 first and then
// nginx's own time_local layout and a couple of convenient shorthands.
func ParseTime(s string) (time.Time, error) {
	var lastErr error
	for _, layout := range nginxTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}
