package tui

import (
	"fmt"
	"time"

	"github.com/rivo/tview"

	"github.com/north21/gonginxlog/internal/stats"
)

func newHeader() *tview.TextView {
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(false)
	return tv
}

// renderHeader must only be called from the main goroutine (inside a
// key handler) or via app.QueueUpdateDraw (inside the tick loop). In
// static mode (live == false) req/s and uptime aren't meaningful (there's
// no tailing/ticking happening at all - see App.Run), so they're omitted
// rather than shown frozen at 0.
func renderHeader(tv *tview.TextView, version, path string, rep *stats.Report, activeAlerts int, startedAt time.Time, reqPerSec float64, live, paused, reloading bool) {
	tv.Clear()

	var state, extra string
	switch {
	case !live:
		state = "[blue::b]■ STATIC[-:-:-]"
	case paused:
		state = "[yellow::b]⏸ PAUSED[-:-:-]"
	default:
		state = "[green::b]● LIVE[-:-:-]"
	}
	if live {
		uptime := time.Since(startedAt).Truncate(time.Second)
		extra = fmt.Sprintf("   req/s: %.0f   uptime: %s", reqPerSec, uptime)
	}

	fmt.Fprintf(tv, " [white::b]gonginxlog %s[-:-:-]  %s  %s   total: %d%s",
		version, path, state, rep.TotalRequests, extra)
	if activeAlerts > 0 {
		fmt.Fprintf(tv, "   [red::b]⚠ %d alert(s)[-:-:-]", activeAlerts)
	}
	if reloading {
		fmt.Fprint(tv, "   [yellow]reloading…[-:-:-]")
	}
}

func newFooter() *tview.TextView {
	tv := tview.NewTextView().SetDynamicColors(true)
	return tv
}

func renderFooterHint(tv *tview.TextView, viewTitle, filterText string, message string) {
	tv.Clear()
	filterDisplay := "none"
	if filterText != "" {
		filterDisplay = filterText
	}
	fmt.Fprintf(tv, " VIEW: %s   filter: %s", viewTitle, filterDisplay)
	if message != "" {
		fmt.Fprintf(tv, "   [yellow]%s[-:-:-]", message)
	}
}
