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

// RouteTimingEntry is one row of the route-grouped average-latency
// table, sorted slowest-first. Only populated when a path labeler was
// installed via Aggregator.SetPathLabeler.
type RouteTimingEntry struct {
	Route      string
	Count      int
	AvgSeconds float64
}

// UpstreamEntry is one row of the "Upstreams" table: which backend
// address, how many requests it got, its average response time (from
// $upstream_response_time, last-attempt-only - see
// Record.UpstreamResponseTimeLast), and how many of those got a
// $upstream_status >= 500. Addr is the literal "-" for requests that
// never reached an upstream at all (served from cache or a static
// file) - AvgSeconds/ErrorCount are meaningless for that row (TimedCount
// will be 0).
type UpstreamEntry struct {
	Addr       string
	Count      int
	AvgSeconds float64
	TimedCount int
	ErrorCount int
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

	// RouteTiming is nil unless a path labeler was configured (see
	// Aggregator.SetPathLabeler) - route grouping is what keeps this
	// bounded, so there is deliberately no raw-path fallback.
	RouteTiming []RouteTimingEntry

	// Upstreams is nil unless Aggregator.SetTrackUpstream(true) was
	// called, which main.go only does when the log_format actually
	// carries $upstream_addr.
	Upstreams []UpstreamEntry
}
