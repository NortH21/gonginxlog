package stats

import "sort"

// upstreamAccum is O(1)-memory per upstream address, the same
// average-not-percentile tradeoff as routeTimingAccum - upstream pools
// are expected to be small and stable (a handful of backend
// addresses), so this isn't chasing a cardinality problem, just
// keeping the two "per-bounded-key timing" features consistent.
type upstreamAccum struct {
	count      int // all requests attributed to this upstream (or "-" for none)
	errorCount int // requests whose $upstream_status was >= 500
	timeSum    float64
	timeN      int // requests that had a $upstream_response_time to average
}

func upstreamEntries(m map[string]*upstreamAccum) []UpstreamEntry {
	entries := make([]UpstreamEntry, 0, len(m))
	for addr, acc := range m {
		var avg float64
		if acc.timeN > 0 {
			avg = acc.timeSum / float64(acc.timeN)
		}
		entries = append(entries, UpstreamEntry{
			Addr:       addr,
			Count:      acc.count,
			AvgSeconds: avg,
			TimedCount: acc.timeN,
			ErrorCount: acc.errorCount,
		})
	}
	// Busiest first, like Top IPs/paths - this table answers "where
	// does traffic go", not "what's slowest" (that's AvgSeconds, a
	// column here, not the sort key).
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Addr < entries[j].Addr
	})
	return entries
}
