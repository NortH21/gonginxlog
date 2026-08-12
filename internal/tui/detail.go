package tui

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/rivo/tview"

	"github.com/north21/gonginxlog/internal/anomaly"
	"github.com/north21/gonginxlog/internal/record"
	"github.com/north21/gonginxlog/internal/stats"
	"github.com/north21/gonginxlog/internal/term"
)

// matchesDimension checks whether rec belongs to the drill-down key for
// the given dimension. pathLabel is only used for "route" (the
// route-grouped paths view, see internal/routes) - nil elsewhere.
func matchesDimension(rec *record.Record, dimension, key string, pathLabel func(string) string) bool {
	switch dimension {
	case "ip":
		return rec.RemoteAddr() == key
	case "path":
		return rec.Path() == key
	case "route":
		if pathLabel == nil {
			return false
		}
		return pathLabel(rec.Path()) == key
	case "country":
		// Aggregator.Add labels unresolved countries as the literal "-"
		// (see internal/stats), but Record.Country returns "" for those
		// - normalize the same way here so drilling into the "-" row
		// actually matches its records instead of always finding zero.
		country := rec.Country()
		if country == "" {
			country = "-"
		}
		return country == key
	case "user_agent":
		return rec.UserAgent() == key
	case "referer":
		return rec.Referer() == key
	case "status":
		code, ok := rec.StatusCode()
		return ok && strconv.Itoa(code) == key
	default:
		return false
	}
}

// alertDimension maps an anomaly type to the dimension/key its Alert.Key
// refers to, so pressing Enter on an alerts-view row can drill down too.
func alertDimension(t anomaly.Type) string {
	switch t {
	case anomaly.TypeIPFlood, anomaly.TypeURLScan:
		return "ip"
	case anomaly.TypeHammer:
		return "path"
	default:
		return ""
	}
}

// breakdown is one "what else" table to show alongside the status table
// in a drill-down page.
type breakdown struct {
	label   string
	extract func(*record.Record) string
}

// complementaryBreakdowns picks which "what else" breakdown(s) to show
// alongside the status table in a drill-down page. Drilling into an IP
// or a path/route only needs the other one; drilling into anything else
// (status, country, user agent, referer) shows both, since either can
// be the actionable answer to "who/what is behind this" (e.g. status
// 500 -> which IPs hit it *and* which paths raised it).
//
// pathLabel, when set, groups the path breakdown into routes too
// (labeled "ROUTE" instead of "PATH") - same reasoning as everywhere
// else route grouping applies: bounded, consistent labels instead of
// unbounded raw paths.
func complementaryBreakdowns(dimension string, pathLabel func(path string) string) []breakdown {
	byIP := breakdown{"IP", func(r *record.Record) string { return r.RemoteAddr() }}
	pathColumn := "PATH"
	pathExtract := func(r *record.Record) string { return r.Path() }
	if pathLabel != nil {
		pathColumn = "ROUTE"
		pathExtract = func(r *record.Record) string { return pathLabel(r.Path()) }
	}
	byPath := breakdown{pathColumn, pathExtract}
	switch dimension {
	case "ip":
		return []breakdown{byPath}
	case "path", "route":
		return []breakdown{byIP}
	case "status", "country", "user_agent", "referer":
		return []breakdown{byIP, byPath}
	default:
		return nil
	}
}

// buildDetailPage builds a self-contained drill-down page for one
// dimension/key pair from records already known to match it (the caller
// filters the ring buffer before calling this). allTimeCount is the
// key's count from the live (unbounded) Aggregator, if known - passing
// a negative value skips the "how much of this is actually in the
// buffer" note (used when there's no such total to compare against,
// e.g. drilling in from the alerts view).
func buildDetailPage(dimension, key string, bufCap int, matched []Entry, trackCountry bool, allTimeCount int, pathLabel func(path string) string) tview.Primitive {
	agg := stats.NewAggregator(0, stats.DisabledBucket, trackCountry)
	if pathLabel != nil {
		agg.SetPathLabeler(pathLabel)
	}
	for _, e := range matched {
		agg.Add(e.Record)
	}
	rep := agg.Report()

	// key can be an attacker-controlled log field (a path or User-Agent
	// drilled into) - sanitize control bytes and escape tview's own
	// "[tag]" markup once, up front, so every place key gets displayed
	// below is safe by construction.
	safeKey := tview.Escape(term.Sanitize(key))

	header := tview.NewTextView().SetDynamicColors(true)
	fmt.Fprintf(header, " [white::b]%s[-:-:-] = %s   %d request(s) among the last %d buffered", dimension, safeKey, len(matched), bufCap)
	if allTimeCount >= 0 && len(matched) < allTimeCount {
		fmt.Fprintf(header, "   [yellow](%d total all-time - the rest are older than the buffer)[-:-:-]", allTimeCount)
	}
	fmt.Fprint(header, "\n")

	statusEntries := make([]stats.CountEntry, 0, len(rep.StatusDist))
	for code, c := range rep.StatusDist {
		statusEntries = append(statusEntries, stats.CountEntry{Key: strconv.Itoa(code), Count: c})
	}
	sortCountEntriesDesc(statusEntries)
	statusView := &viewDef{table: newCountTable()}
	renderCountTable(statusView, statusEntries, rep.TotalRequests, "STATUS")

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 2, 0, false).
		AddItem(statusView.table, 0, 1, true)

	for _, b := range complementaryBreakdowns(dimension, pathLabel) {
		compCounts := map[string]int{}
		for _, e := range matched {
			if v := b.extract(e.Record); v != "" {
				compCounts[v]++
			}
		}
		compEntries := make([]stats.CountEntry, 0, len(compCounts))
		for k, c := range compCounts {
			compEntries = append(compEntries, stats.CountEntry{Key: k, Count: c})
		}
		sortCountEntriesDesc(compEntries)
		if len(compEntries) > 20 {
			compEntries = compEntries[:20]
		}
		compView := &viewDef{table: newCountTable()}
		renderCountTable(compView, compEntries, len(matched), b.label)
		flex.AddItem(compView.table, 0, 1, false)
	}

	flex.SetBorder(true).SetTitle(fmt.Sprintf(" detail: %s=%s (Esc to go back) ", dimension, safeKey))
	return flex
}

func sortCountEntriesDesc(entries []stats.CountEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Key < entries[j].Key
	})
}
