package stats

import (
	"fmt"
	"testing"

	"github.com/north21/gonginxlog/internal/record"
)

func rec(path, requestTime string) *record.Record {
	return &record.Record{Fields: map[string]string{
		"request":      "GET " + path + " HTTP/1.1",
		"request_time": requestTime,
		"status":       "200",
	}}
}

func TestNoPathLabelerLeavesRouteTimingNil(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.Add(rec("/foo", "0.1"))
	rep := a.Report()
	if rep.RouteTiming != nil {
		t.Fatalf("expected RouteTiming to stay nil without SetPathLabeler, got %+v", rep.RouteTiming)
	}
	if got := rep.TopPaths[0].Key; got != "/foo" {
		t.Fatalf("expected raw path /foo without a labeler, got %q", got)
	}
}

func TestPathLabelerGroupsCountsAndTiming(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.SetPathLabeler(func(path string) string {
		if path == "/gamecenter/game/1" || path == "/gamecenter/game/2" {
			return "game_page"
		}
		return "other"
	})

	a.Add(rec("/gamecenter/game/1", "0.10"))
	a.Add(rec("/gamecenter/game/2", "0.30"))
	a.Add(rec("/static/app.js", "0.02"))

	rep := a.Report()

	// pathCounts should be keyed by the label, not the raw path.
	if len(rep.TopPaths) != 2 {
		t.Fatalf("expected 2 distinct route keys, got %+v", rep.TopPaths)
	}

	if rep.RouteTiming == nil {
		t.Fatalf("expected RouteTiming to be populated once a labeler is set")
	}
	if len(rep.RouteTiming) != 2 {
		t.Fatalf("expected 2 route timing entries, got %+v", rep.RouteTiming)
	}
	// Slowest first: game_page avg=(0.10+0.30)/2=0.20, other avg=0.02.
	if rep.RouteTiming[0].Route != "game_page" {
		t.Fatalf("expected game_page first (slowest), got %+v", rep.RouteTiming)
	}
	if got := rep.RouteTiming[0].AvgSeconds; got < 0.199 || got > 0.201 {
		t.Fatalf("expected game_page avg ~0.20, got %v", got)
	}
	if rep.RouteTiming[0].Count != 2 {
		t.Fatalf("expected game_page count 2, got %d", rep.RouteTiming[0].Count)
	}
	if rep.RouteTiming[1].Route != "other" {
		t.Fatalf("expected other second, got %+v", rep.RouteTiming)
	}
}

// TestTopPathsTrimsQueryByDefault covers the default (no --routes-file,
// no --show-path-args) behavior: query strings are trimmed before
// counting, so /foo?x=1 and /foo?y=2 collapse into one "/foo" entry
// instead of each being its own effectively-unique row.
func TestTopPathsTrimsQueryByDefault(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.Add(rec("/foo?x=1", "0.1"))
	a.Add(rec("/foo?y=2", "0.1"))
	rep := a.Report()
	if len(rep.TopPaths) != 1 {
		t.Fatalf("expected the two queries to collapse into one path, got %+v", rep.TopPaths)
	}
	if got := rep.TopPaths[0]; got.Key != "/foo" || got.Count != 2 {
		t.Fatalf("expected {/foo 2}, got %+v", got)
	}
}

func TestTopPathsKeepsQueryWhenConfigured(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.SetKeepPathQuery(true)
	a.Add(rec("/foo?x=1", "0.1"))
	a.Add(rec("/foo?y=2", "0.1"))
	rep := a.Report()
	if len(rep.TopPaths) != 2 {
		t.Fatalf("expected the query strings to be kept as distinct paths, got %+v", rep.TopPaths)
	}
}

// TestPathLabelerAlwaysSeesFullPathRegardlessOfKeepPathQuery: a
// configured --routes-file labeler decides its own handling of query
// strings via its own regex, so it must always receive the raw,
// untrimmed path - keepPathQuery only applies to the fallback
// (labeler-less) case.
func TestPathLabelerAlwaysSeesFullPathRegardlessOfKeepPathQuery(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	var seen string
	a.SetPathLabeler(func(path string) string {
		seen = path
		return "route"
	})
	a.Add(rec("/foo?x=1", "0.1"))
	if seen != "/foo?x=1" {
		t.Fatalf("expected the labeler to see the full path+query, got %q", seen)
	}
}

func TestPathLabelerBoundsRouteTimingMemory(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	labels := []string{"a", "b", "c"}
	a.SetPathLabeler(func(path string) string {
		return labels[len(path)%len(labels)]
	})

	// 10,000 distinct raw paths, but only 3 possible labels - route
	// timing memory must stay at 3 entries regardless of raw-path
	// cardinality. This is the guarantee the whole feature exists for.
	for i := 0; i < 10000; i++ {
		a.Add(rec(fmt.Sprintf("/counter?id=%d", i), "0.05"))
	}

	rep := a.Report()
	if len(rep.RouteTiming) > 3 {
		t.Fatalf("expected route timing bounded to <=3 labels regardless of 10000 distinct raw paths, got %d entries", len(rep.RouteTiming))
	}
	total := 0
	for _, e := range rep.RouteTiming {
		total += e.Count
	}
	if total != 10000 {
		t.Fatalf("expected route timing counts to sum to 10000, got %d", total)
	}
}
