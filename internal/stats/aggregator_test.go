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

func upstreamRec(addr, status, upstreamTime string) *record.Record {
	return &record.Record{Fields: map[string]string{
		"request":                "GET /foo HTTP/1.1",
		"status":                 "200",
		"upstream_addr":          addr,
		"upstream_status":        status,
		"upstream_response_time": upstreamTime,
	}}
}

func TestNoTrackUpstreamLeavesUpstreamsNil(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.Add(upstreamRec("10.0.0.1:8080", "200", "0.05"))
	rep := a.Report()
	if rep.Upstreams != nil {
		t.Fatalf("expected Upstreams to stay nil without SetTrackUpstream, got %+v", rep.Upstreams)
	}
}

func TestTrackUpstreamAggregatesCountAvgAndErrorRate(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.SetTrackUpstream(true)

	a.Add(upstreamRec("10.0.0.1:8080", "200", "0.100"))
	a.Add(upstreamRec("10.0.0.1:8080", "200", "0.200"))
	a.Add(upstreamRec("10.0.0.1:8080", "500", "0.300"))
	a.Add(upstreamRec("10.0.0.2:8080", "200", "0.010"))

	rep := a.Report()
	if len(rep.Upstreams) != 2 {
		t.Fatalf("expected 2 upstream entries, got %+v", rep.Upstreams)
	}
	// Busiest first: 10.0.0.1:8080 has 3 requests, 10.0.0.2:8080 has 1.
	first := rep.Upstreams[0]
	if first.Addr != "10.0.0.1:8080" || first.Count != 3 {
		t.Fatalf("expected {10.0.0.1:8080 count=3} first, got %+v", first)
	}
	if got := first.AvgSeconds; got < 0.199 || got > 0.201 {
		t.Fatalf("expected avg ~0.200 ((0.1+0.2+0.3)/3), got %v", got)
	}
	if first.ErrorCount != 1 {
		t.Fatalf("expected 1 error (the 500), got %d", first.ErrorCount)
	}

	second := rep.Upstreams[1]
	if second.Addr != "10.0.0.2:8080" || second.Count != 1 || second.ErrorCount != 0 {
		t.Fatalf("expected {10.0.0.2:8080 count=1 errors=0}, got %+v", second)
	}
}

func TestTrackUpstreamUsesLastValueOnRetry(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.SetTrackUpstream(true)
	// nginx retried from .1 (which 502'd) to .2 (which answered 200) -
	// the request should be attributed to .2, the one that actually
	// answered, not both and not the first attempt.
	a.Add(upstreamRec("10.0.0.1:8080, 10.0.0.2:8080", "502, 200", "0.050, 0.020"))

	rep := a.Report()
	if len(rep.Upstreams) != 1 {
		t.Fatalf("expected exactly 1 upstream entry (attributed to the last attempt), got %+v", rep.Upstreams)
	}
	e := rep.Upstreams[0]
	if e.Addr != "10.0.0.2:8080" {
		t.Fatalf("expected attribution to 10.0.0.2:8080 (the one that answered), got %q", e.Addr)
	}
	if e.ErrorCount != 0 {
		t.Fatalf("expected 0 errors (the final status was 200, not the earlier 502), got %d", e.ErrorCount)
	}
	if e.AvgSeconds < 0.019 || e.AvgSeconds > 0.021 {
		t.Fatalf("expected avg ~0.020 (last attempt's own time, not the 0.070 sum), got %v", e.AvgSeconds)
	}
}

func TestTrackUpstreamNoUpstreamGetsDashBucket(t *testing.T) {
	a := NewAggregator(0, DisabledBucket, false)
	a.SetTrackUpstream(true)
	a.Add(upstreamRec("-", "-", "-"))
	a.Add(upstreamRec("10.0.0.1:8080", "200", "0.05"))

	rep := a.Report()
	if len(rep.Upstreams) != 2 {
		t.Fatalf("expected 2 entries (one real upstream, one \"-\"), got %+v", rep.Upstreams)
	}
	found := false
	for _, e := range rep.Upstreams {
		if e.Addr == "-" {
			found = true
			if e.Count != 1 || e.TimedCount != 0 || e.ErrorCount != 0 {
				t.Fatalf("expected the \"-\" row to have count=1, no timing, no errors, got %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("expected a \"-\" entry for the no-upstream request, got %+v", rep.Upstreams)
	}
}
