package stats

import (
	"time"

	"github.com/north21/gonginxlog/internal/record"
)

// Aggregator accumulates matched records and produces a Report on demand.
type Aggregator struct {
	topN   int
	bucket time.Duration

	total int

	ipCounts     map[string]int
	pathCounts   map[string]int
	uaCounts     map[string]int
	refCounts    map[string]int
	statusCounts map[int]int

	requestTimes  []float64
	upstreamTimes []float64
	bytesTotal    int64

	histogram        map[int64]*HistogramBucket
	minTime, maxTime time.Time
}

// NewAggregator creates an Aggregator. topN controls how many rows each top
// list keeps. bucket controls the histogram granularity; 0 disables the
// histogram.
func NewAggregator(topN int, bucket time.Duration) *Aggregator {
	return &Aggregator{
		topN:         topN,
		bucket:       bucket,
		ipCounts:     map[string]int{},
		pathCounts:   map[string]int{},
		uaCounts:     map[string]int{},
		refCounts:    map[string]int{},
		statusCounts: map[int]int{},
		histogram:    map[int64]*HistogramBucket{},
	}
}

// Add folds one matched record into the running aggregates.
func (a *Aggregator) Add(r *record.Record) {
	a.total++

	if ip := r.RemoteAddr(); ip != "" {
		a.ipCounts[ip]++
	}
	if p := r.Path(); p != "" {
		a.pathCounts[p]++
	}
	if ua := r.UserAgent(); ua != "" {
		a.uaCounts[ua]++
	}
	if ref := r.Referer(); ref != "" {
		a.refCounts[ref]++
	}
	if code, ok := r.StatusCode(); ok {
		a.statusCounts[code]++
	}
	if rt, ok := r.RequestTime(); ok {
		a.requestTimes = append(a.requestTimes, rt)
	}
	if ut, ok := r.UpstreamResponseTime(); ok {
		a.upstreamTimes = append(a.upstreamTimes, ut)
	}
	bytesSent, hasBytes := r.BytesSent()
	if hasBytes {
		a.bytesTotal += bytesSent
	}

	t, ok := r.Time()
	if !ok {
		return
	}
	if a.minTime.IsZero() || t.Before(a.minTime) {
		a.minTime = t
	}
	if a.maxTime.IsZero() || t.After(a.maxTime) {
		a.maxTime = t
	}
	if a.bucket > 0 {
		start := t.Truncate(a.bucket)
		key := start.Unix()
		hb, ok := a.histogram[key]
		if !ok {
			hb = &HistogramBucket{Start: start}
			a.histogram[key] = hb
		}
		hb.Count++
		if hasBytes {
			hb.Bytes += bytesSent
		}
	}
}

// Report snapshots the current aggregates into a Report.
func (a *Aggregator) Report() *Report {
	return &Report{
		TotalRequests:  a.total,
		From:           a.minTime,
		To:             a.maxTime,
		StatusDist:     a.statusCounts,
		TopIPs:         topN(a.ipCounts, a.topN),
		TopPaths:       topN(a.pathCounts, a.topN),
		TopUserAgents:  topN(a.uaCounts, a.topN),
		TopReferers:    topN(a.refCounts, a.topN),
		RequestTiming:  summarize(a.requestTimes),
		UpstreamTiming: summarize(a.upstreamTimes),
		BytesSentTotal: a.bytesTotal,
		Histogram:      sortedHistogram(a.histogram),
	}
}
