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
	if !matchesDimension(withoutCountry, "country", "-", nil) {
		t.Fatalf("expected a record with no country to match dimension=country key=\"-\"")
	}

	withCountry := &record.Record{Fields: map[string]string{"remote_addr": "203.0.113.5", "geoip_country_code": "RU"}}
	if matchesDimension(withCountry, "country", "-", nil) {
		t.Fatalf("a record with a resolved country must not match key=\"-\"")
	}
	if !matchesDimension(withCountry, "country", "RU", nil) {
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
	if !matchesDimension(rec, "route", "game_page", label) {
		t.Fatalf("expected route match via the labeler")
	}
	if matchesDimension(rec, "route", "other", label) {
		t.Fatalf("expected no match for a different route label")
	}
	if matchesDimension(rec, "route", "game_page", nil) {
		t.Fatalf("expected no match when pathLabel is nil")
	}
}

func TestComplementaryBreakdownsBothWaysForStatus(t *testing.T) {
	// Drilling into a status code (or country/UA/referer) should offer
	// both an IP and a PATH breakdown - either can be the actionable
	// answer to "what's causing this".
	got := complementaryBreakdowns("status", nil)
	if len(got) != 2 || got[0].label != "IP" || got[1].label != "PATH" {
		t.Fatalf("expected [IP, PATH] breakdowns for dimension=status, got %+v", labelsOf(got))
	}
}

func TestComplementaryBreakdownsSingleForIPAndPath(t *testing.T) {
	if got := complementaryBreakdowns("ip", nil); len(got) != 1 || got[0].label != "PATH" {
		t.Fatalf("expected only [PATH] for dimension=ip, got %+v", labelsOf(got))
	}
	if got := complementaryBreakdowns("path", nil); len(got) != 1 || got[0].label != "IP" {
		t.Fatalf("expected only [IP] for dimension=path, got %+v", labelsOf(got))
	}
}

func TestComplementaryBreakdownsPathLabelUsesRouteColumn(t *testing.T) {
	label := func(path string) string { return "grouped" }
	got := complementaryBreakdowns("status", label)
	if len(got) != 2 || got[1].label != "ROUTE" {
		t.Fatalf("expected the path breakdown labeled ROUTE when pathLabel is set, got %+v", labelsOf(got))
	}
	rec := &record.Record{Fields: map[string]string{"request": "GET /anything HTTP/1.1"}}
	if got[1].extract(rec) != "grouped" {
		t.Fatalf("expected the ROUTE column to use the labeler, got %q", got[1].extract(rec))
	}
}

func labelsOf(bs []breakdown) []string {
	labels := make([]string, len(bs))
	for i, b := range bs {
		labels[i] = b.label
	}
	return labels
}
