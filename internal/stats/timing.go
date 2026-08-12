package stats

import (
	"math"
	"sort"
)

// summarize computes count/avg/percentiles/max over a set of durations
// (seconds). The input is not assumed sorted; a sorted copy is made.
func summarize(values []float64) TimingSummary {
	n := len(values)
	if n == 0 {
		return TimingSummary{}
	}
	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}

	return TimingSummary{
		Count: n,
		Avg:   sum / float64(n),
		P50:   percentile(sorted, 50),
		P90:   percentile(sorted, 90),
		P99:   percentile(sorted, 99),
		Max:   sorted[n-1],
	}
}

// percentile assumes sorted is sorted ascending and non-empty.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
