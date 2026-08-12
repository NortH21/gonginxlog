package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/north21/gonginxlog/internal/anomaly"
	"github.com/north21/gonginxlog/internal/stats"
)

// viewDef is one switchable view: a hotkey, a title, and (for the
// count-table views) a "dimension" name used to drive drill-down.
// dimension is empty for views Enter doesn't do anything special on
// (raw, timeline) or that have their own Enter handling (alerts).
type viewDef struct {
	id        string
	title     string
	key       rune
	dimension string
	primitive tview.Primitive
	table     *tview.Table
	text      *tview.TextView
	entries   []stats.CountEntry // row (minus header) -> CountEntry, refreshed each render
}

func headerCell(text string) *tview.TableCell {
	return tview.NewTableCell(text).
		SetSelectable(false).
		SetStyle(tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true))
}

func percentStr(n, total int) string {
	if total <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(total))
}

func barString(count, max, width int) string {
	if max <= 0 || count <= 0 {
		return ""
	}
	n := int(float64(count) / float64(max) * float64(width))
	if n == 0 {
		n = 1
	}
	return strings.Repeat("█", n)
}

func newCountTable() *tview.Table {
	t := tview.NewTable().SetBorders(false)
	t.SetFixed(1, 0).SetSelectable(true, false)
	t.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray))
	return t
}

// renderCountTable repopulates table from entries (already in the order
// the caller wants displayed) and refreshes the view's drill-down cache.
func renderCountTable(v *viewDef, entries []stats.CountEntry, total int, keyHeader string) {
	v.entries = entries
	t := v.table
	t.Clear()
	t.SetCell(0, 0, headerCell("#"))
	t.SetCell(0, 1, headerCell(keyHeader))
	t.SetCell(0, 2, headerCell("COUNT"))
	t.SetCell(0, 3, headerCell("%"))
	t.SetCell(0, 4, headerCell(""))

	max := 0
	for _, e := range entries {
		if e.Count > max {
			max = e.Count
		}
	}
	for i, e := range entries {
		row := i + 1
		t.SetCell(row, 0, tview.NewTableCell(strconv.Itoa(row)))
		t.SetCell(row, 1, tview.NewTableCell(e.Key))
		t.SetCell(row, 2, tview.NewTableCell(strconv.Itoa(e.Count)).SetAlign(tview.AlignRight))
		t.SetCell(row, 3, tview.NewTableCell(percentStr(e.Count, total)).SetAlign(tview.AlignRight))
		t.SetCell(row, 4, tview.NewTableCell(barString(e.Count, max, 24)).SetTextColor(tcell.ColorGreen))
	}
}

// renderStatusTable renders the status-code distribution as a
// CountEntry table too (sorted by code, not by count), so the same
// drill-down machinery works uniformly across every view.
func renderStatusTable(v *viewDef, dist map[int]int, total int) {
	codes := make([]int, 0, len(dist))
	for c := range dist {
		codes = append(codes, c)
	}
	sort.Ints(codes)

	entries := make([]stats.CountEntry, len(codes))
	for i, c := range codes {
		entries[i] = stats.CountEntry{Key: strconv.Itoa(c), Count: dist[c]}
	}
	v.entries = entries

	t := v.table
	t.Clear()
	t.SetCell(0, 0, headerCell("STATUS"))
	t.SetCell(0, 1, headerCell("COUNT"))
	t.SetCell(0, 2, headerCell("%"))
	t.SetCell(0, 3, headerCell(""))

	max := 0
	for _, c := range codes {
		if dist[c] > max {
			max = dist[c]
		}
	}
	for i, c := range codes {
		row := i + 1
		cnt := dist[c]
		t.SetCell(row, 0, tview.NewTableCell(strconv.Itoa(c)).SetTextColor(statusColor(c)).SetAttributes(tcell.AttrBold))
		t.SetCell(row, 1, tview.NewTableCell(strconv.Itoa(cnt)).SetAlign(tview.AlignRight))
		t.SetCell(row, 2, tview.NewTableCell(percentStr(cnt, total)).SetAlign(tview.AlignRight))
		t.SetCell(row, 3, tview.NewTableCell(barString(cnt, max, 24)).SetTextColor(statusColor(c)))
	}
}

func renderTimeline(tv *tview.TextView, buckets []stats.HistogramBucket) {
	tv.Clear()
	if len(buckets) == 0 {
		fmt.Fprint(tv, " (not enough data yet)")
		return
	}
	max := 0
	for _, b := range buckets {
		if b.Count > max {
			max = b.Count
		}
	}
	for _, b := range buckets {
		fmt.Fprintf(tv, " %s  %6d  [green]%s[-:-:-]\n", b.Start.Format("2006-01-02 15:04"), b.Count, barString(b.Count, max, 50))
	}
}

func renderAlertsTable(t *tview.Table, alerts []anomaly.Alert) {
	t.Clear()
	t.SetCell(0, 0, headerCell("STATE"))
	t.SetCell(0, 1, headerCell("TYPE"))
	t.SetCell(0, 2, headerCell("KEY"))
	t.SetCell(0, 3, headerCell("DETAIL"))
	t.SetCell(0, 4, headerCell("SINCE"))

	if len(alerts) == 0 {
		t.SetCell(1, 0, tview.NewTableCell("(no anomalies observed yet)").SetSelectable(false))
		return
	}
	for i, a := range alerts {
		row := i + 1
		state := "resolved"
		if a.Active {
			state = "ACTIVE"
		}
		color := alertColor(a.Active)
		t.SetCell(row, 0, tview.NewTableCell(state).SetTextColor(color).SetAttributes(tcell.AttrBold))
		t.SetCell(row, 1, tview.NewTableCell(string(a.Type)).SetTextColor(color))
		t.SetCell(row, 2, tview.NewTableCell(a.Key).SetTextColor(color))
		t.SetCell(row, 3, tview.NewTableCell(a.Detail).SetTextColor(color))
		t.SetCell(row, 4, tview.NewTableCell(a.FirstSeen.Format("15:04:05")).SetTextColor(color))
	}
}
