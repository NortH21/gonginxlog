package stats

import (
	"time"

	"github.com/north21/gonginxlog/internal/record"
)

// Bucket controls the requests-over-time histogram granularity: either a
// fixed duration, or Auto to pick a sensible size from the observed time
// span (like goaccess adapting to how much data there is), or Disabled to
// skip the histogram entirely.
type Bucket struct {
	Auto     bool
	Disabled bool
	Fixed    time.Duration
}

// AutoBucket picks the bucket size automatically from the data's time span.
var AutoBucket = Bucket{Auto: true}

// DisabledBucket turns the histogram off.
var DisabledBucket = Bucket{Disabled: true}

// FixedBucket uses exactly d as the bucket size.
func FixedBucket(d time.Duration) Bucket { return Bucket{Fixed: d} }

// autoBucketCandidates are tried smallest first; the first one that keeps
// the bucket count at or below autoBucketTarget wins.
var autoBucketCandidates = []time.Duration{
	time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

const autoBucketTarget = 20

// timedSample is one record's contribution to the histogram, kept aside so
// bucket size can be decided once the full time span is known.
type timedSample struct {
	at    time.Time
	bytes int64
}

// Aggregator accumulates matched records and produces a Report on demand.
type Aggregator struct {
	topN   int
	bucket Bucket

	total int

	ipCounts     map[string]int
	pathCounts   map[string]int
	uaCounts     map[string]int
	refCounts    map[string]int
	statusCounts map[int]int

	requestTimes  []float64
	upstreamTimes []float64
	bytesTotal    int64

	samples          []timedSample
	minTime, maxTime time.Time
}

// NewAggregator creates an Aggregator. topN controls how many rows each top
// list keeps. bucket controls the requests-over-time histogram granularity.
func NewAggregator(topN int, bucket Bucket) *Aggregator {
	return &Aggregator{
		topN:         topN,
		bucket:       bucket,
		ipCounts:     map[string]int{},
		pathCounts:   map[string]int{},
		uaCounts:     map[string]int{},
		refCounts:    map[string]int{},
		statusCounts: map[int]int{},
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
	if !a.bucket.Disabled {
		a.samples = append(a.samples, timedSample{at: t, bytes: bytesSent})
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
		Histogram:      a.histogram(),
	}
}

func (a *Aggregator) histogram() []HistogramBucket {
	if a.bucket.Disabled || len(a.samples) == 0 {
		return nil
	}
	d := a.bucket.Fixed
	if a.bucket.Auto {
		d = pickAutoBucket(a.maxTime.Sub(a.minTime))
	}
	if d <= 0 {
		return nil
	}

	buckets := map[int64]*HistogramBucket{}
	for _, s := range a.samples {
		start := s.at.Truncate(d)
		key := start.Unix()
		hb, ok := buckets[key]
		if !ok {
			hb = &HistogramBucket{Start: start}
			buckets[key] = hb
		}
		hb.Count++
		hb.Bytes += s.bytes
	}
	return sortedHistogram(buckets)
}

// pickAutoBucket returns the smallest candidate duration that keeps the
// number of buckets over span at or below autoBucketTarget.
func pickAutoBucket(span time.Duration) time.Duration {
	for _, c := range autoBucketCandidates {
		if span/c <= autoBucketTarget {
			return c
		}
	}
	return autoBucketCandidates[len(autoBucketCandidates)-1]
}
