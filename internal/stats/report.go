// Package stats aggregates matched records into a summary report: status
// code distribution, top IPs/paths/user-agents/referers, request/upstream
// timing percentiles, and a requests-over-time histogram.
package stats

import "time"

// CountEntry is one row of a "top N" table.
type CountEntry struct {
	Key   string
	Count int
}

// TimingSummary summarizes a set of durations (seconds).
type TimingSummary struct {
	Count                   int
	Avg, P50, P90, P99, Max float64
}

// HistogramBucket is one requests-over-time bucket.
type HistogramBucket struct {
	Start time.Time
	Count int
	Bytes int64
}

// Report is the fully aggregated view produced by Aggregator.Report.
type Report struct {
	TotalRequests int
	From, To      time.Time

	StatusDist map[int]int

	TopIPs        []CountEntry
	TopPaths      []CountEntry
	TopUserAgents []CountEntry
	TopReferers   []CountEntry

	// TopCountries is nil when the log_format has neither
	// $geoip_country_code nor $geoip2_data_country_code.
	TopCountries []CountEntry

	RequestTiming  TimingSummary
	UpstreamTiming TimingSummary

	BytesSentTotal int64

	Histogram []HistogramBucket
}
