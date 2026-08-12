package stats

import "sort"

func sortedHistogram(m map[int64]*HistogramBucket) []HistogramBucket {
	out := make([]HistogramBucket, 0, len(m))
	for _, hb := range m {
		out = append(out, *hb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}
