package stats

import "sort"

// topN returns the n counts with the highest value, ties broken by key so
// the output order is stable across runs.
func topN(counts map[string]int, n int) []CountEntry {
	entries := make([]CountEntry, 0, len(counts))
	for k, c := range counts {
		entries = append(entries, CountEntry{Key: k, Count: c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Key < entries[j].Key
	})
	if n > 0 && len(entries) > n {
		entries = entries[:n]
	}
	return entries
}
