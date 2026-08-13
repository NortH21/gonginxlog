package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/north21/gonginxlog/internal/stats"
)

func sampleReport() *stats.Report {
	return &stats.Report{
		TotalRequests: 10,
		StatusDist:    map[int]int{200: 8, 500: 2},
		TopIPs:        []stats.CountEntry{{Key: "203.0.113.5", Count: 10}},
		TopPaths:      []stats.CountEntry{{Key: "/foo", Count: 10}},
		TopUserAgents: []stats.CountEntry{{Key: "curl/8.0", Count: 10}},
		TopReferers:   []stats.CountEntry{{Key: "https://example.com", Count: 10}},
	}
}

func TestWriteTextNoColorForNonFileWriter(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, sampleReport(), Options{})
	if strings.Contains(buf.String(), "\033[") {
		t.Fatalf("expected no ANSI escape codes when writing to a non-*os.File, got:\n%s", buf.String())
	}
}

func TestWriteTextAgentsRefererersOffByDefault(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, sampleReport(), Options{})
	out := buf.String()
	if strings.Contains(out, "Top user agents") {
		t.Fatalf("expected Top user agents to be hidden by default, got:\n%s", out)
	}
	if strings.Contains(out, "Top referers") {
		t.Fatalf("expected Top referers to be hidden by default, got:\n%s", out)
	}
}

func TestWriteTextAgentsReferersShownWhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, sampleReport(), Options{ShowAgents: true, ShowReferers: true})
	out := buf.String()
	if !strings.Contains(out, "Top user agents") {
		t.Fatalf("expected Top user agents to appear when ShowAgents is true, got:\n%s", out)
	}
	if !strings.Contains(out, "Top referers") {
		t.Fatalf("expected Top referers to appear when ShowReferers is true, got:\n%s", out)
	}
}

func TestWriteTextRouteTimingSection(t *testing.T) {
	rep := sampleReport()
	rep.RouteTiming = []stats.RouteTimingEntry{
		{Route: "game_page", Count: 5, AvgSeconds: 0.234},
	}
	var buf bytes.Buffer
	WriteText(&buf, rep, Options{})
	out := buf.String()
	if !strings.Contains(out, "Slowest routes") {
		t.Fatalf("expected a 'Slowest routes' section when RouteTiming is set, got:\n%s", out)
	}
	if !strings.Contains(out, "game_page") {
		t.Fatalf("expected the route label in the output, got:\n%s", out)
	}
}

func TestWriteTextNoRouteTimingSectionWhenNil(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, sampleReport(), Options{})
	if strings.Contains(buf.String(), "Slowest routes") {
		t.Fatalf("expected no 'Slowest routes' section when RouteTiming is nil")
	}
}

func TestWriteTextRendersWithoutPanicOnEmptyReport(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, &stats.Report{}, Options{ShowAgents: true, ShowReferers: true})
}

// TestWriteTextSanitizesControlBytesInFields covers a real attack: a
// path/User-Agent/referer is attacker-controlled data (nginx logs an
// HTTP request's fields verbatim), so an ESC byte injected there must
// never reach the terminal unsanitized - it could otherwise forge or
// hide output for whoever reads the report.
func TestWriteTextSanitizesControlBytesInFields(t *testing.T) {
	rep := &stats.Report{
		TotalRequests: 1,
		TopPaths:      []stats.CountEntry{{Key: "/evil\x1b[31mred\x1b[0m", Count: 1}},
		RouteTiming:   []stats.RouteTimingEntry{{Route: "evil\x1broute", Count: 1, AvgSeconds: 0.1}},
	}
	var buf bytes.Buffer
	WriteText(&buf, rep, Options{})
	out := buf.String()
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("expected no raw ESC bytes in the report output, got:\n%q", out)
	}
	if !strings.Contains(out, "/evil[31mred[0m") {
		t.Fatalf("expected the sanitized path (control bytes stripped, rest kept) in the output, got:\n%s", out)
	}
	if !strings.Contains(out, "evilroute") {
		t.Fatalf("expected the sanitized route label in the output, got:\n%s", out)
	}
}

func TestWriteTextUpstreamsSection(t *testing.T) {
	rep := sampleReport()
	rep.Upstreams = []stats.UpstreamEntry{
		{Addr: "10.0.0.1:8080", Count: 8, AvgSeconds: 0.042, TimedCount: 8, ErrorCount: 1},
		{Addr: "-", Count: 2, TimedCount: 0, ErrorCount: 0},
	}
	var buf bytes.Buffer
	WriteText(&buf, rep, Options{})
	out := buf.String()
	if !strings.Contains(out, "Upstreams") {
		t.Fatalf("expected an 'Upstreams' section when Upstreams is set, got:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.1:8080") {
		t.Fatalf("expected the upstream address in the output, got:\n%s", out)
	}
	if !strings.Contains(out, "12.5%") {
		t.Fatalf("expected the 1/8 error rate (12.5%%) in the output, got:\n%s", out)
	}
	if !strings.Contains(out, "no upstream") {
		t.Fatalf("expected the \"-\" row to be labeled as no upstream, got:\n%s", out)
	}
}

func TestWriteTextNoUpstreamsSectionWhenNil(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, sampleReport(), Options{})
	if strings.Contains(buf.String(), "Upstreams") {
		t.Fatalf("expected no 'Upstreams' section when Upstreams is nil")
	}
}
