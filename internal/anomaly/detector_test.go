package anomaly

import (
	"testing"
	"time"
)

func TestIPFloodTriggers(t *testing.T) {
	d := NewDetector(DefaultThresholds)
	base := time.Now()

	// 14 requests from the flooding IP, 6 from others: total is 20 (at
	// the min-sample floor) and the flooding IP's 70% share clears the
	// 30% threshold.
	for i := 0; i < 14; i++ {
		d.Observe("1.2.3.4", "/x", base)
	}
	for i := 0; i < 6; i++ {
		d.Observe("5.6.7.8", "/x", base)
	}

	alerts := d.Tick(base)
	found := false
	for _, a := range alerts {
		if a.Type == TypeIPFlood && a.Key == "1.2.3.4" && a.Active {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an active ip_flood alert for 1.2.3.4, got %+v", alerts)
	}
}

func TestIPFloodBelowMinSampleDoesNotTrigger(t *testing.T) {
	d := NewDetector(DefaultThresholds)
	base := time.Now()

	// Same 70/30 split, but total is only 10... wait we need below
	// floodMinTotal (20). Use 4 total requests, all from one IP: 100%
	// share but too few samples to be meaningful.
	for i := 0; i < 4; i++ {
		d.Observe("1.2.3.4", "/x", base)
	}

	alerts := d.Tick(base)
	for _, a := range alerts {
		if a.Type == TypeIPFlood && a.Active {
			t.Fatalf("expected no active ip_flood alert below the min-sample floor, got %+v", a)
		}
	}
}

func TestURLScanTriggers(t *testing.T) {
	d := NewDetector(DefaultThresholds)
	base := time.Now()

	for i := 0; i < DefaultThresholds.ScanPaths; i++ {
		d.Observe("9.9.9.9", "/path"+string(rune('a'+i)), base)
	}

	alerts := d.Tick(base)
	found := false
	for _, a := range alerts {
		if a.Type == TypeURLScan && a.Key == "9.9.9.9" && a.Active {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an active url_scan alert for 9.9.9.9, got %+v", alerts)
	}
}

func TestDistributedHammerTriggers(t *testing.T) {
	d := NewDetector(DefaultThresholds)
	base := time.Now()

	for i := 0; i < DefaultThresholds.HammerIPs; i++ {
		ip := "10.0.0." + string(rune('1'+i))
		d.Observe(ip, "/login", base)
	}

	alerts := d.Tick(base)
	found := false
	for _, a := range alerts {
		if a.Type == TypeHammer && a.Key == "/login" && a.Active {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an active distributed_hammer alert for /login, got %+v", alerts)
	}
}

func TestAlertResolvesWhenNoLongerTriggering(t *testing.T) {
	d := NewDetector(DefaultThresholds)
	base := time.Now()

	for i := 0; i < 14; i++ {
		d.Observe("1.2.3.4", "/x", base)
	}
	for i := 0; i < 6; i++ {
		d.Observe("5.6.7.8", "/x", base)
	}
	alerts := d.Tick(base)
	if !alertActive(alerts, TypeIPFlood, "1.2.3.4") {
		t.Fatalf("expected flood alert to be active initially")
	}

	// Tick again long after the window has elapsed with no new traffic:
	// the buckets evict and the alert should resolve (still present,
	// but Active=false).
	later := base.Add(2 * floodWindow)
	alerts = d.Tick(later)
	if alertActive(alerts, TypeIPFlood, "1.2.3.4") {
		t.Fatalf("expected flood alert to have resolved after the window elapsed, got %+v", alerts)
	}
	foundResolved := false
	for _, a := range alerts {
		if a.Type == TypeIPFlood && a.Key == "1.2.3.4" && !a.Active {
			foundResolved = true
		}
	}
	if !foundResolved {
		t.Fatalf("expected the resolved alert to still be present in history, got %+v", alerts)
	}
}

func TestSeedHistoryDoesNotFalselyTrigger(t *testing.T) {
	// Simulates runUI's seed phase: a burst of old records (by their own
	// timestamps) that would trip every threshold if eviction used
	// record time instead of wall-clock time.
	d := NewDetector(DefaultThresholds)
	seedTime := time.Now().Add(-2 * time.Hour)

	for i := 0; i < 100; i++ {
		d.Observe("1.2.3.4", "/x", seedTime)
	}

	alerts := d.Tick(time.Now())
	for _, a := range alerts {
		if a.Active {
			t.Fatalf("2-hour-old seeded traffic must not trigger a live alert, got %+v", a)
		}
	}
}

func alertActive(alerts []Alert, typ Type, key string) bool {
	for _, a := range alerts {
		if a.Type == typ && a.Key == key && a.Active {
			return true
		}
	}
	return false
}
