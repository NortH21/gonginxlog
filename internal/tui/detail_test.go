package tui

import (
	"testing"

	"github.com/north21/gonginxlog/internal/record"
)

func TestMatchesDimensionCountryDash(t *testing.T) {
	// Aggregator.Add labels an unresolved country as the literal "-",
	// but Record.Country() returns "" for that case - matchesDimension
	// must normalize the same way, or drilling into the "-" row always
	// finds zero matches even though the top-level report shows some.
	withoutCountry := &record.Record{Fields: map[string]string{"remote_addr": "203.0.113.5"}}
	if !matchesDimension(withoutCountry, "country", "-", nil, false) {
		t.Fatalf("expected a record with no country to match dimension=country key=\"-\"")
	}

	withCountry := &record.Record{Fields: map[string]string{"remote_addr": "203.0.113.5", "geoip_country_code": "RU"}}
	if matchesDimension(withCountry, "country", "-", nil, false) {
		t.Fatalf("a record with a resolved country must not match key=\"-\"")
	}
	if !matchesDimension(withCountry, "country", "RU", nil, false) {
		t.Fatalf("expected a record with country=RU to match dimension=country key=\"RU\"")
	}
}

func TestMatchesDimensionRoute(t *testing.T) {
	label := func(path string) string {
		if path == "/gamecenter/game/1" {
			return "game_page"
		}
		return "other"
	}
	rec := &record.Record{Fields: map[string]string{"request": "GET /gamecenter/game/1 HTTP/1.1"}}
	if !matchesDimension(rec, "route", "game_page", label, false) {
		t.Fatalf("expected route match via the labeler")
	}
	if matchesDimension(rec, "route", "other", label, false) {
		t.Fatalf("expected no match for a different route label")
	}
	if matchesDimension(rec, "route", "game_page", nil, false) {
		t.Fatalf("expected no match when pathLabel is nil")
	}
}

// TestMatchesDimensionPathTrimsQueryByDefault covers the "paths" view's
// default behavior (--show-path-args off): the aggregated key is the
// path without its query string, so matching must trim the same way or
// drilling into a "paths" row would always find zero records.
func TestMatchesDimensionPathTrimsQueryByDefault(t *testing.T) {
	rec := &record.Record{Fields: map[string]string{"request": "GET /api/profile.php?stand=0&id=123 HTTP/1.1"}}
	if !matchesDimension(rec, "path", "/api/profile.php", nil, false) {
		t.Fatalf("expected the query-trimmed key to match by default")
	}
	if matchesDimension(rec, "path", "/api/profile.php?stand=0&id=123", nil, false) {
		t.Fatalf("expected the full path+query to NOT match by default")
	}
}

func TestMatchesDimensionPathKeepsQueryWhenConfigured(t *testing.T) {
	rec := &record.Record{Fields: map[string]string{"request": "GET /api/profile.php?stand=0 HTTP/1.1"}}
	if !matchesDimension(rec, "path", "/api/profile.php?stand=0", nil, true) {
		t.Fatalf("expected the full path+query to match when keepPathQuery is true")
	}
	if matchesDimension(rec, "path", "/api/profile.php", nil, true) {
		t.Fatalf("expected the trimmed key to NOT match when keepPathQuery is true")
	}
}

func TestComplementaryBreakdownsBothWaysForStatus(t *testing.T) {
	// Drilling into a status code (or country/UA/referer) should offer
	// both an IP and a PATH breakdown - either can be the actionable
	// answer to "what's causing this".
	got := complementaryBreakdowns("status", nil, false)
	if len(got) != 2 || got[0].label != "IP" || got[1].label != "PATH" {
		t.Fatalf("expected [IP, PATH] breakdowns for dimension=status, got %+v", labelsOf(got))
	}
}

func TestComplementaryBreakdownsSingleForIPAndPath(t *testing.T) {
	if got := complementaryBreakdowns("ip", nil, false); len(got) != 1 || got[0].label != "PATH" {
		t.Fatalf("expected only [PATH] for dimension=ip, got %+v", labelsOf(got))
	}
	if got := complementaryBreakdowns("path", nil, false); len(got) != 1 || got[0].label != "IP" {
		t.Fatalf("expected only [IP] for dimension=path, got %+v", labelsOf(got))
	}
}

func TestComplementaryBreakdownsPathLabelUsesRouteColumn(t *testing.T) {
	label := func(path string) string { return "grouped" }
	got := complementaryBreakdowns("status", label, false)
	if len(got) != 2 || got[1].label != "ROUTE" {
		t.Fatalf("expected the path breakdown labeled ROUTE when pathLabel is set, got %+v", labelsOf(got))
	}
	rec := &record.Record{Fields: map[string]string{"request": "GET /anything HTTP/1.1"}}
	if got[1].extract(rec) != "grouped" {
		t.Fatalf("expected the ROUTE column to use the labeler, got %q", got[1].extract(rec))
	}
}

// TestComplementaryBreakdownsPathColumnTrimsQueryByDefault covers the
// PATH breakdown column shown alongside e.g. a status/country/UA/
// referer drill-down: it should match the same query-trimming default
// as the top-level "paths" view, unless keepPathQuery is set.
func TestComplementaryBreakdownsPathColumnTrimsQueryByDefault(t *testing.T) {
	rec := &record.Record{Fields: map[string]string{"request": "GET /foo?x=1 HTTP/1.1"}}

	trimmed := complementaryBreakdowns("status", nil, false)
	if got := trimmed[1].extract(rec); got != "/foo" {
		t.Fatalf("expected the PATH column to trim the query string by default, got %q", got)
	}

	kept := complementaryBreakdowns("status", nil, true)
	if got := kept[1].extract(rec); got != "/foo?x=1" {
		t.Fatalf("expected the PATH column to keep the query string when keepPathQuery is true, got %q", got)
	}
}

func labelsOf(bs []breakdown) []string {
	labels := make([]string, len(bs))
	for i, b := range bs {
		labels[i] = b.label
	}
	return labels
}
