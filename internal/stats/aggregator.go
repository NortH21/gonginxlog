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
	topN         int
	bucket       Bucket
	trackCountry bool

	total int

	ipCounts      map[string]int
	pathCounts    map[string]int
	uaCounts      map[string]int
	refCounts     map[string]int
	statusCounts  map[int]int
	countryCounts map[string]int

	// pathLabeler, when set via SetPathLabeler, remaps each record's raw
	// path into a bounded "route" label before it's counted, and turns
	// on per-route average-timing tracking. nil means "use raw paths",
	// the original/default behavior.
	pathLabeler  func(path string) string
	routeTimings map[string]*routeTimingAccum

	// keepPathQuery controls whether the "paths" top-N table keys on
	// the full request path including its query string, or (the
	// default, keepPathQuery == false, so the zero value is already
	// correct) the path alone via Record.PathWithoutQuery. Only applies
	// when pathLabeler is nil - a configured --routes-file labeler
	// always gets the raw, untrimmed path, since its regexes may
	// deliberately match against query parameters.
	keepPathQuery bool

	// trackUpstream, set via SetTrackUpstream, enables the "Upstreams"
	// table (Report().Upstreams). Off by default: not every log_format
	// carries $upstream_addr, and unlike routes/paths this doesn't need
	// a bounded-cardinality guard since upstream pools are expected to
	// be small and stable (a handful of backend addresses) - see
	// DESIGN.md.
	trackUpstream  bool
	upstreamCounts map[string]*upstreamAccum

	requestTimes  []float64
	upstreamTimes []float64
	bytesTotal    int64

	samples          []timedSample
	minTime, maxTime time.Time
}

// NewAggregator creates an Aggregator. topN controls how many rows each top
// list keeps. bucket controls the requests-over-time histogram granularity.
// trackCountry should be true only when the log_format actually carries
// $geoip_country_code/$geoip2_data_country_code, so the report doesn't show
// a misleading all-"-" country table for formats that don't have it.
func NewAggregator(topN int, bucket Bucket, trackCountry bool) *Aggregator {
	a := &Aggregator{
		topN:         topN,
		bucket:       bucket,
		trackCountry: trackCountry,
		ipCounts:     map[string]int{},
		pathCounts:   map[string]int{},
		uaCounts:     map[string]int{},
		refCounts:    map[string]int{},
		statusCounts: map[int]int{},
	}
	if trackCountry {
		a.countryCounts = map[string]int{}
	}
	return a
}

// SetPathLabeler installs fn to remap every record's path into a
// "route" label before it's used as the top-paths key, and enables
// per-route average-timing tracking (Report().RouteTiming). Memory for
// route timings is bounded by the number of distinct labels fn
// returns, not by request volume or raw path cardinality - callers
// should use a small, fixed label set (see internal/routes), never an
// unbounded function like "return the raw path unchanged".
func (a *Aggregator) SetPathLabeler(fn func(path string) string) {
	a.pathLabeler = fn
	if a.routeTimings == nil {
		a.routeTimings = map[string]*routeTimingAccum{}
	}
}

// SetKeepPathQuery controls the "paths" table's default query-string
// trimming (see keepPathQuery's doc comment). keep == true restores
// the pre-trimming behavior of counting the full path+query verbatim.
func (a *Aggregator) SetKeepPathQuery(keep bool) {
	a.keepPathQuery = keep
}

// SetTrackUpstream enables the "Upstreams" table (see trackUpstream's
// doc comment).
func (a *Aggregator) SetTrackUpstream(track bool) {
	a.trackUpstream = track
	if track && a.upstreamCounts == nil {
		a.upstreamCounts = map[string]*upstreamAccum{}
	}
}

// Add folds one matched record into the running aggregates.
func (a *Aggregator) Add(r *record.Record) {
	a.total++

	if ip := r.RemoteAddr(); ip != "" {
		a.ipCounts[ip]++
	}

	path := r.Path()
	if a.pathLabeler != nil {
		path = a.pathLabeler(path)
	} else if !a.keepPathQuery {
		path = r.PathWithoutQuery()
	}
	if path != "" {
		a.pathCounts[path]++
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
	if a.trackCountry {
		country := r.Country()
		if country == "" {
			country = "-"
		}
		a.countryCounts[country]++
	}
	rt, hasRT := r.RequestTime()
	if hasRT {
		a.requestTimes = append(a.requestTimes, rt)
	}
	if a.pathLabeler != nil && hasRT && path != "" {
		acc := a.routeTimings[path]
		if acc == nil {
			acc = &routeTimingAccum{}
			a.routeTimings[path] = acc
		}
		acc.sum += rt
		acc.n++
	}
	if ut, ok := r.UpstreamResponseTime(); ok {
		a.upstreamTimes = append(a.upstreamTimes, ut)
	}
	if a.trackUpstream {
		addr := r.UpstreamAddr()
		if addr == "" {
			addr = "-"
		}
		acc := a.upstreamCounts[addr]
		if acc == nil {
			acc = &upstreamAccum{}
			a.upstreamCounts[addr] = acc
		}
		acc.count++
		if code, ok := r.UpstreamStatus(); ok && code >= 500 {
			acc.errorCount++
		}
		if ut, ok := r.UpstreamResponseTimeLast(); ok {
			acc.timeSum += ut
			acc.timeN++
		}
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

// Report snapshots the current aggregates into a Report. The returned
// Report must be a fully independent copy - callers (notably the live
// TUI) hold onto it and read it from other goroutines after Report()
// returns and its caller has released whatever lock guarded Add(), so
// any field that aliased Aggregator's own state instead of copying it
// would be a live data race (this bit us once already: StatusDist used
// to be assigned directly from a.statusCounts).
func (a *Aggregator) Report() *Report {
	statusDist := make(map[int]int, len(a.statusCounts))
	for code, count := range a.statusCounts {
		statusDist[code] = count
	}

	rep := &Report{
		TotalRequests:  a.total,
		From:           a.minTime,
		To:             a.maxTime,
		StatusDist:     statusDist,
		TopIPs:         topN(a.ipCounts, a.topN),
		TopPaths:       topN(a.pathCounts, a.topN),
		TopUserAgents:  topN(a.uaCounts, a.topN),
		TopReferers:    topN(a.refCounts, a.topN),
		RequestTiming:  summarize(a.requestTimes),
		UpstreamTiming: summarize(a.upstreamTimes),
		BytesSentTotal: a.bytesTotal,
		Histogram:      a.histogram(),
	}
	if a.trackCountry {
		rep.TopCountries = topN(a.countryCounts, a.topN)
	}
	if a.pathLabeler != nil {
		rep.RouteTiming = routeTimingEntries(a.routeTimings)
	}
	if a.trackUpstream {
		rep.Upstreams = upstreamEntries(a.upstreamCounts)
	}
	return rep
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
