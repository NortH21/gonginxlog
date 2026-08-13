package record

import "testing"

func TestPathWithoutQueryTrimsAtFirstQuestionMark(t *testing.T) {
	r := &Record{Fields: map[string]string{"request": "GET /api/profile.php?stand=0&id=123 HTTP/1.1"}}
	if got := r.PathWithoutQuery(); got != "/api/profile.php" {
		t.Fatalf("PathWithoutQuery() = %q, want %q", got, "/api/profile.php")
	}
}

func TestPathWithoutQueryLeavesPlainPathUnchanged(t *testing.T) {
	r := &Record{Fields: map[string]string{"request": "GET /foo/bar HTTP/1.1"}}
	if got := r.PathWithoutQuery(); got != "/foo/bar" {
		t.Fatalf("PathWithoutQuery() = %q, want %q", got, "/foo/bar")
	}
}

func TestPathWithoutQueryOnEmptyRequest(t *testing.T) {
	r := &Record{Fields: map[string]string{}}
	if got := r.PathWithoutQuery(); got != "" {
		t.Fatalf("PathWithoutQuery() = %q, want empty", got)
	}
}

func TestUpstreamAddrSingleValue(t *testing.T) {
	r := &Record{Fields: map[string]string{"upstream_addr": "10.0.0.1:8080"}}
	if got := r.UpstreamAddr(); got != "10.0.0.1:8080" {
		t.Fatalf("UpstreamAddr() = %q, want %q", got, "10.0.0.1:8080")
	}
}

func TestUpstreamAddrTakesLastOnRetry(t *testing.T) {
	r := &Record{Fields: map[string]string{"upstream_addr": "10.0.0.1:8080, 10.0.0.2:8080"}}
	if got := r.UpstreamAddr(); got != "10.0.0.2:8080" {
		t.Fatalf("UpstreamAddr() = %q, want the last (retried-to) address %q", got, "10.0.0.2:8080")
	}
}

func TestUpstreamAddrNoUpstream(t *testing.T) {
	r := &Record{Fields: map[string]string{"upstream_addr": "-"}}
	if got := r.UpstreamAddr(); got != "" {
		t.Fatalf("UpstreamAddr() = %q, want empty for \"-\"", got)
	}
	r2 := &Record{Fields: map[string]string{}}
	if got := r2.UpstreamAddr(); got != "" {
		t.Fatalf("UpstreamAddr() = %q, want empty when the field is absent", got)
	}
}

func TestUpstreamStatusTakesLastOnRetry(t *testing.T) {
	r := &Record{Fields: map[string]string{"upstream_status": "502, 200"}}
	code, ok := r.UpstreamStatus()
	if !ok || code != 200 {
		t.Fatalf("UpstreamStatus() = (%d, %v), want (200, true)", code, ok)
	}
}

func TestUpstreamStatusAbsent(t *testing.T) {
	r := &Record{Fields: map[string]string{"upstream_status": "-"}}
	if _, ok := r.UpstreamStatus(); ok {
		t.Fatalf("expected UpstreamStatus() to report absent for \"-\"")
	}
}

func TestUpstreamResponseTimeLastVsSum(t *testing.T) {
	r := &Record{Fields: map[string]string{"upstream_response_time": "0.100, 0.030"}}
	last, ok := r.UpstreamResponseTimeLast()
	if !ok || last != 0.030 {
		t.Fatalf("UpstreamResponseTimeLast() = (%v, %v), want (0.030, true)", last, ok)
	}
	sum, ok := r.UpstreamResponseTime()
	if !ok || sum < 0.129 || sum > 0.131 {
		t.Fatalf("UpstreamResponseTime() (sum) = (%v, %v), want ~0.130", sum, ok)
	}
}
