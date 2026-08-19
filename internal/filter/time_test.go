package filter

import (
	"testing"
	"time"
)

// TestParseTime_BareLayoutsUseLocalZone guards against a regression where
// --since/--until values with no zone offset (e.g. "2026-08-19T15:13:00")
// were parsed as UTC by time.Parse's default behavior instead of the
// machine's local zone, silently comparing against the wrong instant on
// any host not itself running in UTC.
func TestParseTime_BareLayoutsUseLocalZone(t *testing.T) {
	cases := []string{
		"2026-08-19 15:13:00",
		"2026-08-19T15:13:00",
	}
	for _, s := range cases {
		got, err := ParseTime(s)
		if err != nil {
			t.Fatalf("ParseTime(%q): %v", s, err)
		}
		if _, off := got.Zone(); off != 0 {
			// Local zone offset varies by test machine; what matters is
			// that it matches time.Local, not that it's any particular
			// value.
			_, wantOff := time.Now().Zone()
			if off != wantOff {
				t.Errorf("ParseTime(%q) zone offset = %d, want local offset %d", s, off, wantOff)
			}
		}
		if got.Location() != time.Local {
			t.Errorf("ParseTime(%q) location = %v, want time.Local", s, got.Location())
		}
	}
}

// TestParseTime_ZonedLayoutsKeepTheirOffset ensures values that carry an
// explicit zone (RFC3339 with "Z" or an offset, or nginx's own
// time_local format) are parsed exactly as written, not shifted to local
// time.
func TestParseTime_ZonedLayoutsKeepTheirOffset(t *testing.T) {
	got, err := ParseTime("2026-08-19T15:13:00Z")
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	if !got.Equal(time.Date(2026, 8, 19, 15, 13, 0, 0, time.UTC)) {
		t.Errorf("ParseTime(RFC3339 Z) = %v, want 2026-08-19T15:13:00Z", got)
	}

	got, err = ParseTime("19/Aug/2026:15:13:00 +0300")
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	wantLoc := time.FixedZone("", 3*60*60)
	if !got.Equal(time.Date(2026, 8, 19, 15, 13, 0, 0, wantLoc)) {
		t.Errorf("ParseTime(nginx time_local) = %v, want 2026-08-19T15:13:00+03:00", got)
	}
}
