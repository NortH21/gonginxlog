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
// key handler) or via app.QueueUpdateDraw (inside the tick loop).
func renderHeader(tv *tview.TextView, path string, rep *stats.Report, activeAlerts int, startedAt time.Time, reqPerSec float64, reloading bool) {
	tv.Clear()
	uptime := time.Since(startedAt).Truncate(time.Second)
	fmt.Fprintf(tv, " [white::b]gonginxlog[-:-:-]  %s  [green::b]● LIVE[-:-:-]   req/s: %.0f   total: %d   uptime: %s",
		path, reqPerSec, rep.TotalRequests, uptime)
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

func hotkeyBar() string {
	return " [yellow]1[-:-:-] status  [yellow]2[-:-:-] ips  [yellow]3[-:-:-] countries  [yellow]4[-:-:-] paths  " +
		"[yellow]5[-:-:-] agents  [yellow]6[-:-:-] referers  [yellow]7[-:-:-] timeline  [yellow]l[-:-:-] raw  [yellow]a[-:-:-] alerts  " +
		"[yellow]/[-:-:-] filter  [yellow]x[-:-:-] clear  [yellow]Enter[-:-:-] detail  [yellow]Esc[-:-:-] back  [yellow]q[-:-:-] quit"
}
