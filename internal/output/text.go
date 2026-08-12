// Package output renders a stats.Report as either human-readable text
// tables (with goaccess-ish ASCII bars) or JSON.
package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/north21/gonginxlog/internal/stats"
)

// WriteText renders rep as text tables to w.
func WriteText(w io.Writer, rep *stats.Report) {
	fmt.Fprintf(w, "Requests: %d", rep.TotalRequests)
	if !rep.From.IsZero() {
		fmt.Fprintf(w, "  (%s .. %s)", rep.From.Format(time.RFC3339), rep.To.Format(time.RFC3339))
	}
	fmt.Fprint(w, "\n\n")

	writeStatusDist(w, rep)
	writeTopTable(w, "Top client IPs", rep.TopIPs, rep.TotalRequests)
	writeTopTable(w, "Top requested paths", rep.TopPaths, rep.TotalRequests)
	writeTopTable(w, "Top user agents", rep.TopUserAgents, rep.TotalRequests)
	writeTopTable(w, "Top referers", rep.TopReferers, rep.TotalRequests)
	writeTiming(w, "Request time (s)", rep.RequestTiming)
	writeTiming(w, "Upstream response time (s)", rep.UpstreamTiming)
	if rep.BytesSentTotal > 0 {
		fmt.Fprintf(w, "Bytes sent total: %s\n\n", humanBytes(rep.BytesSentTotal))
	}
	writeHistogram(w, rep.Histogram)
}

func writeStatusDist(w io.Writer, rep *stats.Report) {
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
		fmt.Fprintf(tw, "  %d\t%d\t%s\t%s\n", c, cnt, percentOf(cnt, rep.TotalRequests), bar(cnt, max, 30))
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func writeTopTable(w io.Writer, title string, entries []stats.CountEntry, total int) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	max := entries[0].Count
	for i, e := range entries {
		fmt.Fprintf(tw, "  %2d.\t%s\t%d\t%s\t%s\n", i+1, e.Key, e.Count, percentOf(e.Count, total), bar(e.Count, max, 24))
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
		fmt.Fprintf(w, "  %s  %6d  %s\n", b.Start.Format("2006-01-02 15:04"), b.Count, bar(b.Count, max, 40))
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
