// Package output renders a stats.Report as either human-readable text
// tables (with goaccess-ish ASCII bars) or JSON.
package output

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/north21/gonginxlog/internal/stats"
	"github.com/north21/gonginxlog/internal/term"
)

// Options controls which optional sections WriteText prints.
type Options struct {
	ShowAgents   bool
	ShowReferers bool
}

// WriteText renders rep as text tables to w.
func WriteText(w io.Writer, rep *stats.Report, opts Options) {
	useColor := term.UseColor(asFile(w))

	fmt.Fprintf(w, "Requests: %d", rep.TotalRequests)
	if !rep.From.IsZero() {
		fmt.Fprintf(w, "  (%s .. %s)", rep.From.Format(time.RFC3339), rep.To.Format(time.RFC3339))
	}
	fmt.Fprint(w, "\n\n")

	writeStatusDist(w, rep, useColor)
	writeTopTable(w, "Top client IPs", rep.TopIPs, rep.TotalRequests)
	writeTopTable(w, "Top countries", rep.TopCountries, rep.TotalRequests)
	writeTopTable(w, "Top requested paths", rep.TopPaths, rep.TotalRequests)
	if opts.ShowAgents {
		writeTopTable(w, "Top user agents", rep.TopUserAgents, rep.TotalRequests)
	}
	if opts.ShowReferers {
		writeTopTable(w, "Top referers", rep.TopReferers, rep.TotalRequests)
	}
	writeTiming(w, "Request time (s)", rep.RequestTiming)
	writeTiming(w, "Upstream response time (s)", rep.UpstreamTiming)
	writeUpstreams(w, rep.Upstreams, rep.TotalRequests)
	if rep.BytesSentTotal > 0 {
		fmt.Fprintf(w, "Bytes sent total: %s\n\n", humanBytes(rep.BytesSentTotal))
	}
	writeRouteTiming(w, rep.RouteTiming)
	writeHistogram(w, rep.Histogram)
}

// asFile returns w as an *os.File if it is one (so terminal/color
// detection can inspect it), or nil otherwise - a non-file writer (a
// buffer, a network connection, ...) never gets colorized.
func asFile(w io.Writer) *os.File {
	f, _ := w.(*os.File)
	return f
}

func writeStatusDist(w io.Writer, rep *stats.Report, useColor bool) {
	if len(rep.StatusDist) == 0 {
		return
	}
	fmt.Fprintln(w, "Status codes")
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	codes := make([]int, 0, len(rep.StatusDist))
	for c := range rep.StatusDist {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	max := 0
	for _, c := range codes {
		if rep.StatusDist[c] > max {
			max = rep.StatusDist[c]
		}
	}
	for _, c := range codes {
		cnt := rep.StatusDist[c]
		code := term.Colorize(useColor, statusColor(c), fmt.Sprintf("%d", c))
		fmt.Fprintf(tw, "  %s\t%d\t%s\t%s\n", code, cnt, percentOf(cnt, rep.TotalRequests), bar(cnt, max, 30))
	}
	tw.Flush()
	fmt.Fprintln(w)
}

// statusColor mirrors internal/tui/colors.go's status-class mapping
// (2xx green, 3xx cyan, 4xx/5xx yellow/red) as ANSI codes instead of
// tcell.Color, since batch text output isn't a tview widget.
func statusColor(code int) string {
	switch {
	case code >= 500:
		return term.Red
	case code >= 400:
		return term.Yellow
	case code >= 300:
		return term.Cyan
	case code >= 200:
		return term.Green
	default:
		return ""
	}
}

func writeTopTable(w io.Writer, title string, entries []stats.CountEntry, total int) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	max := entries[0].Count
	for i, e := range entries {
		fmt.Fprintf(tw, "  %2d.\t%s\t%d\t%s\t%s\n", i+1, term.Sanitize(e.Key), e.Count, percentOf(e.Count, total), bar(e.Count, max, 24))
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func writeTiming(w io.Writer, label string, t stats.TimingSummary) {
	if t.Count == 0 {
		return
	}
	fmt.Fprintf(w, "%s (n=%d): avg=%.3f p50=%.3f p90=%.3f p99=%.3f max=%.3f\n\n",
		label, t.Count, t.Avg, t.P50, t.P90, t.P99, t.Max)
}

func writeRouteTiming(w io.Writer, entries []stats.RouteTimingEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(w, "Slowest routes (avg request_time, --routes-file)")
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for i, e := range entries {
		fmt.Fprintf(tw, "  %2d.\t%s\tavg=%.3fs\tn=%d\n", i+1, term.Sanitize(e.Route), e.AvgSeconds, e.Count)
	}
	tw.Flush()
	fmt.Fprintln(w)
}

// writeUpstreams prints one row per backend address ($upstream_addr,
// last-attempt-only - see Record.UpstreamAddr), plus a "-" row for
// requests that never reached an upstream (cache/static). Addr comes
// from nginx's own config, not client-controlled request data, so
// unlike the path/UA/referer tables it doesn't need term.Sanitize.
func writeUpstreams(w io.Writer, entries []stats.UpstreamEntry, total int) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(w, "Upstreams")
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  #\tADDR\tREQS\t%\tAVG\tERR%")
	for i, e := range entries {
		addr := e.Addr
		avg, errRate := "-", "-"
		if addr == "-" {
			addr = "- (no upstream)"
		} else {
			if e.TimedCount > 0 {
				avg = fmt.Sprintf("%.3fs", e.AvgSeconds)
			}
			if e.Count > 0 {
				errRate = fmt.Sprintf("%.1f%%", 100*float64(e.ErrorCount)/float64(e.Count))
			}
		}
		fmt.Fprintf(tw, "  %2d.\t%s\t%d\t%s\t%s\t%s\n", i+1, addr, e.Count, percentOf(e.Count, total), avg, errRate)
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func writeHistogram(w io.Writer, buckets []stats.HistogramBucket) {
	if len(buckets) == 0 {
		return
	}
	fmt.Fprintln(w, "Requests over time")
	max := 0
	for _, b := range buckets {
		if b.Count > max {
			max = b.Count
		}
	}
	for _, b := range buckets {
		fmt.Fprintf(w, "  %s  %6d  %s\n", b.Start.Format("2006-01-02 15:04 -0700"), b.Count, bar(b.Count, max, 40))
	}
	fmt.Fprintln(w)
}

func percentOf(n, total int) string {
	if total <= 0 {
		return "  -   "
	}
	return fmt.Sprintf("%5.1f%%", 100*float64(n)/float64(total))
}

func bar(count, max, width int) string {
	if max <= 0 || count <= 0 {
		return ""
	}
	n := int(float64(count) / float64(max) * float64(width))
	if n == 0 {
		n = 1
	}
	return strings.Repeat("█", n)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
