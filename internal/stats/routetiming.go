package stats

import "sort"

// routeTimingAccum is an O(1)-memory running average, not a percentile -
// exact percentiles would need per-route sample storage, and route
// cardinality (bounded by the configured label set) doesn't buy back
// enough to justify the extra memory for that.
type routeTimingAccum struct {
	sum float64
	n   int
}

func routeTimingEntries(m map[string]*routeTimingAccum) []RouteTimingEntry {
	entries := make([]RouteTimingEntry, 0, len(m))
	for route, acc := range m {
		if acc.n == 0 {
			continue
		}
		entries = append(entries, RouteTimingEntry{
			Route:      route,
			Count:      acc.n,
			AvgSeconds: acc.sum / float64(acc.n),
		})
	}
	// Slowest first - that's the point of this table, unlike the
	// count-sorted Top* fields.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].AvgSeconds != entries[j].AvgSeconds {
			return entries[i].AvgSeconds > entries[j].AvgSeconds
		}
		return entries[i].Route < entries[j].Route
	})
	return entries
}
