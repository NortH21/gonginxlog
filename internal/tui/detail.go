package tui

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/rivo/tview"

	"github.com/north21/gonginxlog/internal/anomaly"
	"github.com/north21/gonginxlog/internal/record"
	"github.com/north21/gonginxlog/internal/stats"
)

// matchesDimension checks whether rec belongs to the drill-down key for
// the given dimension - the same six dimensions the switchable views
// use (the five CountEntry-backed ones plus "status").
func matchesDimension(rec *record.Record, dimension, key string) bool {
	switch dimension {
	case "ip":
		return rec.RemoteAddr() == key
	case "path":
		return rec.Path() == key
	case "country":
		return rec.Country() == key
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

// complementaryDimension picks which "what else" breakdown to show
// alongside the status table in a drill-down page - e.g. drilling into
// an IP shows the paths it hit; drilling into a path shows the IPs that
// hit it.
func complementaryDimension(dimension string) (label string, extract func(*record.Record) string) {
	switch dimension {
	case "path":
		return "IP", func(r *record.Record) string { return r.RemoteAddr() }
	case "ip", "user_agent", "referer", "country", "status":
		return "PATH", func(r *record.Record) string { return r.Path() }
	default:
		return "", nil
	}
}

// buildDetailPage builds a self-contained drill-down page for one
// dimension/key pair from records already known to match it (the caller
// filters the ring buffer before calling this).
func buildDetailPage(dimension, key string, bufCap int, matched []Entry, trackCountry bool) tview.Primitive {
	agg := stats.NewAggregator(0, stats.DisabledBucket, trackCountry)
	for _, e := range matched {
		agg.Add(e.Record)
	}
	rep := agg.Report()

	header := tview.NewTextView().SetDynamicColors(true)
	fmt.Fprintf(header, " [white::b]%s[-:-:-] = %s   %d request(s) among the last %d buffered\n", dimension, key, len(matched), bufCap)

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

	if compLabel, extract := complementaryDimension(dimension); extract != nil {
		compCounts := map[string]int{}
		for _, e := range matched {
			if v := extract(e.Record); v != "" {
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
		renderCountTable(compView, compEntries, len(matched), compLabel)
		flex.AddItem(compView.table, 0, 1, false)
	}

	flex.SetBorder(true).SetTitle(fmt.Sprintf(" detail: %s=%s (Esc to go back) ", dimension, key))
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
