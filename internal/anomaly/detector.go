// Package anomaly implements simple, fixed-threshold sliding-window
// detectors for the live TUI: one IP flooding traffic, one IP scanning
// many distinct paths, and one path being hammered from many distinct
// IPs.
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
	floodWindow   = 60 * time.Second
	floodShare    = 0.30
	floodMinTotal = 20

	scanWindow    = 10 * time.Second
	scanThreshold = 20

	hammerWindow    = 10 * time.Second
	hammerThreshold = 15

	maxAlertHistory = 200
)

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
// evaluates the three fixed thresholds on each Tick.
type Detector struct {
	mu sync.Mutex

	floodBuckets  map[int64]*floodBucket
	scanBuckets   map[int64]*setBucket // ip -> distinct paths seen that second
	hammerBuckets map[int64]*setBucket // path -> distinct ips seen that second

	alerts map[alertKey]*Alert
}

// NewDetector creates an empty Detector.
func NewDetector() *Detector {
	return &Detector{
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
	if total >= floodMinTotal {
		for ip, c := range counts {
			share := float64(c) / float64(total)
			if share >= floodShare {
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
		if len(paths) >= scanThreshold {
			triggered[ip] = fmt.Sprintf("%d distinct paths in last %s", len(paths), scanWindow)
		}
	}
	d.applyTriggered(TypeURLScan, now, triggered)
}

func (d *Detector) checkHammer(now time.Time) {
	union := unionSets(d.hammerBuckets)
	triggered := map[string]string{}
	for path, ips := range union {
		if len(ips) >= hammerThreshold {
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
