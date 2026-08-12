// Package anomaly implements simple sliding-window detectors for the
// live TUI: one IP flooding traffic, one IP scanning many distinct
// paths, and one path being hammered from many distinct IPs. The
// windows (60s/10s/10s) are fixed; the trigger thresholds are
// configurable via Thresholds, since what counts as "too concentrated"
// varies a lot by site - a single legitimate client (a game backend's
// own heartbeat/matchmaking traffic, a health checker, ...) can easily
// clear a 30% share on a quiet endpoint without being an attack.
package anomaly

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Type identifies which detector raised an Alert.
type Type string

const (
	TypeIPFlood Type = "ip_flood"
	TypeURLScan Type = "url_scan"
	TypeHammer  Type = "distributed_hammer"
)

const (
	floodWindow  = 60 * time.Second
	scanWindow   = 10 * time.Second
	hammerWindow = 10 * time.Second

	maxAlertHistory = 200
)

// Thresholds controls when each detector fires. DefaultThresholds
// matches gonginxlog's original fixed values.
type Thresholds struct {
	// FloodShare is the minimum fraction (0..1) of traffic in the last
	// 60s a single IP must account for to trigger ip_flood.
	FloodShare float64
	// FloodMinTotal is the minimum number of requests in that 60s window
	// before FloodShare is even evaluated, so a handful of requests to a
	// quiet site doesn't trip it just because one of them is 100%.
	FloodMinTotal int
	// ScanPaths is the minimum number of distinct paths one IP must hit
	// in the last 10s to trigger url_scan.
	ScanPaths int
	// HammerIPs is the minimum number of distinct IPs hitting one path
	// in the last 10s to trigger distributed_hammer.
	HammerIPs int
}

// DefaultThresholds are gonginxlog's original built-in values.
var DefaultThresholds = Thresholds{
	FloodShare:    0.30,
	FloodMinTotal: 20,
	ScanPaths:     20,
	HammerIPs:     15,
}

// Alert is one detector finding, coalesced by (Type, Key): repeat
// triggers update LastSeen/Detail on the same entry instead of
// duplicating it.
type Alert struct {
	Type      Type
	Key       string
	Detail    string
	FirstSeen time.Time
	LastSeen  time.Time
	Active    bool
}

type alertKey struct {
	typ Type
	key string
}

type floodBucket struct {
	counts map[string]int
	total  int
}

// setBucket tracks, for one second, a key -> set-of-related-values
// mapping. Used for both "distinct paths per IP" (scan) and "distinct
// IPs per path" (hammer) - same shape, different key/value meaning.
type setBucket struct {
	sets map[string]map[string]struct{}
}

// Detector accumulates Observe calls into per-second sliding windows and
// evaluates thresholds on each Tick.
type Detector struct {
	mu sync.Mutex

	thresholds Thresholds

	floodBuckets  map[int64]*floodBucket
	scanBuckets   map[int64]*setBucket // ip -> distinct paths seen that second
	hammerBuckets map[int64]*setBucket // path -> distinct ips seen that second

	alerts map[alertKey]*Alert
}

// NewDetector creates an empty Detector using t as its trigger
// thresholds (see DefaultThresholds).
func NewDetector(t Thresholds) *Detector {
	return &Detector{
		thresholds:    t,
		floodBuckets:  map[int64]*floodBucket{},
		scanBuckets:   map[int64]*setBucket{},
		hammerBuckets: map[int64]*setBucket{},
		alerts:        map[alertKey]*Alert{},
	}
}

// Observe folds one matched record into the sliding windows. recordTime
// should be the record's own timestamp, not wall-clock ingestion time -
// see the package-level note on Tick for why that distinction matters.
func (d *Detector) Observe(ip, path string, recordTime time.Time) {
	if ip == "" || recordTime.IsZero() {
		return
	}
	sec := recordTime.Unix()

	d.mu.Lock()
	defer d.mu.Unlock()

	fb := d.floodBuckets[sec]
	if fb == nil {
		fb = &floodBucket{counts: map[string]int{}}
		d.floodBuckets[sec] = fb
	}
	fb.counts[ip]++
	fb.total++

	if path == "" {
		return
	}

	sb := d.scanBuckets[sec]
	if sb == nil {
		sb = &setBucket{sets: map[string]map[string]struct{}{}}
		d.scanBuckets[sec] = sb
	}
	addToSet(sb.sets, ip, path)

	hb := d.hammerBuckets[sec]
	if hb == nil {
		hb = &setBucket{sets: map[string]map[string]struct{}{}}
		d.hammerBuckets[sec] = hb
	}
	addToSet(hb.sets, path, ip)
}

func addToSet(sets map[string]map[string]struct{}, key, value string) {
	set := sets[key]
	if set == nil {
		set = map[string]struct{}{}
		sets[key] = set
	}
	set[value] = struct{}{}
}

// Tick evicts buckets that have fallen outside their window relative to
// wall-clock now, re-evaluates all three thresholds, and returns a
// snapshot of every known alert (active ones first, newest first).
//
// Eviction uses wall-clock now deliberately, even though Observe buckets
// by each record's own timestamp: a live TUI seeds itself by batch-
// reading a file's existing content (which can be hours of history)
// before it starts tailing. If eviction also used the seeded records'
// timestamps, that seed pass would look like it all happened in the
// same instant and could spuriously trip every threshold. Keying by
// record time but evicting by wall-clock time means old seeded data
// naturally ages out of the 10s/60s windows before Tick ever runs, and
// only genuinely recent (live) activity can trigger.
func (d *Detector) Tick(now time.Time) []Alert {
	d.mu.Lock()
	defer d.mu.Unlock()

	evictFlood(d.floodBuckets, now)
	evictSet(d.scanBuckets, now, scanWindow)
	evictSet(d.hammerBuckets, now, hammerWindow)

	d.checkFlood(now)
	d.checkScan(now)
	d.checkHammer(now)
	d.trimHistory()

	return d.snapshot()
}

func evictFlood(m map[int64]*floodBucket, now time.Time) {
	cutoff := now.Add(-floodWindow).Unix()
	for sec := range m {
		if sec < cutoff {
			delete(m, sec)
		}
	}
}

func evictSet(m map[int64]*setBucket, now time.Time, window time.Duration) {
	cutoff := now.Add(-window).Unix()
	for sec := range m {
		if sec < cutoff {
			delete(m, sec)
		}
	}
}

func (d *Detector) checkFlood(now time.Time) {
	counts := map[string]int{}
	total := 0
	for _, b := range d.floodBuckets {
		total += b.total
		for ip, c := range b.counts {
			counts[ip] += c
		}
	}

	triggered := map[string]string{}
	if total >= d.thresholds.FloodMinTotal {
		for ip, c := range counts {
			share := float64(c) / float64(total)
			if share >= d.thresholds.FloodShare {
				triggered[ip] = fmt.Sprintf("%.0f%% of traffic (%d/%d req) in last %s", share*100, c, total, floodWindow)
			}
		}
	}
	d.applyTriggered(TypeIPFlood, now, triggered)
}

func (d *Detector) checkScan(now time.Time) {
	union := unionSets(d.scanBuckets)
	triggered := map[string]string{}
	for ip, paths := range union {
		if len(paths) >= d.thresholds.ScanPaths {
			triggered[ip] = fmt.Sprintf("%d distinct paths in last %s", len(paths), scanWindow)
		}
	}
	d.applyTriggered(TypeURLScan, now, triggered)
}

func (d *Detector) checkHammer(now time.Time) {
	union := unionSets(d.hammerBuckets)
	triggered := map[string]string{}
	for path, ips := range union {
		if len(ips) >= d.thresholds.HammerIPs {
			triggered[path] = fmt.Sprintf("%d distinct IPs in last %s", len(ips), hammerWindow)
		}
	}
	d.applyTriggered(TypeHammer, now, triggered)
}

func unionSets(buckets map[int64]*setBucket) map[string]map[string]struct{} {
	union := map[string]map[string]struct{}{}
	for _, b := range buckets {
		for key, values := range b.sets {
			dst := union[key]
			if dst == nil {
				dst = map[string]struct{}{}
				union[key] = dst
			}
			for v := range values {
				dst[v] = struct{}{}
			}
		}
	}
	return union
}

// applyTriggered updates/creates alerts for every key in triggered, and
// marks any previously-active alert of this type that isn't in
// triggered anymore as resolved.
func (d *Detector) applyTriggered(typ Type, now time.Time, triggered map[string]string) {
	for key, detail := range triggered {
		ak := alertKey{typ, key}
		a := d.alerts[ak]
		if a == nil {
			a = &Alert{Type: typ, Key: key, FirstSeen: now}
			d.alerts[ak] = a
		}
		a.Detail = detail
		a.LastSeen = now
		a.Active = true
	}
	for ak, a := range d.alerts {
		if ak.typ == typ && a.Active {
			if _, ok := triggered[ak.key]; !ok {
				a.Active = false
			}
		}
	}
}

// trimHistory drops the oldest resolved alerts once the total exceeds
// maxAlertHistory. Active alerts are never dropped.
func (d *Detector) trimHistory() {
	if len(d.alerts) <= maxAlertHistory {
		return
	}
	type entry struct {
		key alertKey
		at  time.Time
	}
	var inactive []entry
	for k, a := range d.alerts {
		if !a.Active {
			inactive = append(inactive, entry{k, a.LastSeen})
		}
	}
	sort.Slice(inactive, func(i, j int) bool { return inactive[i].at.Before(inactive[j].at) })

	excess := len(d.alerts) - maxAlertHistory
	for i := 0; i < excess && i < len(inactive); i++ {
		delete(d.alerts, inactive[i].key)
	}
}

func (d *Detector) snapshot() []Alert {
	out := make([]Alert, 0, len(d.alerts))
	for _, a := range d.alerts {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// ActiveCount reports how many alerts are currently active, for the
// header badge. Safe to call concurrently with Observe/Tick.
func (d *Detector) ActiveCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, a := range d.alerts {
		if a.Active {
			n++
		}
	}
	return n
}
