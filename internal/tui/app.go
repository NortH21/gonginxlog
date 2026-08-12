// Package tui implements gonginxlog's live, k9s-styled dashboard: a
// single full-screen switchable view over a tailed nginx log, with an
// in-view filter bar, per-row drill-down, and anomaly alerts.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/north21/gonginxlog/internal/anomaly"
	"github.com/north21/gonginxlog/internal/filter"
	"github.com/north21/gonginxlog/internal/input"
	"github.com/north21/gonginxlog/internal/parser"
	"github.com/north21/gonginxlog/internal/record"
	"github.com/north21/gonginxlog/internal/stats"
)

// Config configures a TUI session, live or static.
type Config struct {
	Version      string   // shown in the header, e.g. the release tag - "" is fine, just shows blank
	Paths        []string // one file when Live; any number, gzip included, when static
	Live         bool     // false = batch-load Paths once and browse, no tailing/ticking at all
	Parser       parser.Parser
	BaseFilters  filter.And // from CLI startup flags; always ANDed with the in-TUI filter
	TrackCountry bool
	ShowAgents   bool // include the "agents" view (off by default: usually not interesting)
	ShowReferers bool // include the "referers" view (off by default)
	// PathLabel, when set, groups paths into a bounded set of route
	// labels (see internal/routes) - passed straight to
	// stats.Aggregator.SetPathLabeler wherever this package builds one.
	// nil means "use raw paths", and the "routes" view doesn't appear.
	PathLabel    func(path string) string
	BufferSize   int                // ring buffer capacity backing the raw view + drill-down
	Refresh      time.Duration      // stat refresh interval (Live only)
	PollInterval time.Duration      // file-tail poll interval (Live only)
	Anomaly      anomaly.Thresholds // trigger thresholds for the alerts view
}

// App is a live dashboard over one tailed nginx log file.
type App struct {
	cfg Config
	ctx context.Context

	app      *tview.Application
	pages    *tview.Pages
	rootFlex *tview.Flex
	header   *tview.TextView
	footer   *tview.TextView

	filterFocused bool
	paused        bool // set/read from both the key-handler and tick goroutines; guarded by mu

	views      []*viewDef
	activeView int

	mu         sync.Mutex
	agg        *stats.Aggregator
	buf        *RingBuffer
	detector   *anomaly.Detector
	liveFilter filter.And

	liveFilterText string
	startedAt      time.Time
	prevTotal      int
	prevTickAt     time.Time
	lastRate       float64

	lastReport *stats.Report
	lastAlerts []anomaly.Alert
}

// NewApp builds the App and performs the initial seed scan of
// cfg.Path so the dashboard isn't empty on first launch (see DESIGN.md
// for why this matters more than it might seem). ctx is the same
// context that will later be passed to Run; the seed scan honors its
// cancellation too, since it can take a while on a large file.
func NewApp(ctx context.Context, cfg Config) *App {
	if cfg.Refresh <= 0 {
		cfg.Refresh = time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 10000
	}
	if cfg.Anomaly == (anomaly.Thresholds{}) {
		cfg.Anomaly = anomaly.DefaultThresholds
	}

	a := &App{
		cfg:       cfg,
		ctx:       ctx,
		app:       tview.NewApplication(),
		startedAt: time.Now(),
	}

	a.agg, a.buf, a.detector = a.scanFile(ctx, filter.And{})
	a.lastReport = a.agg.Report()
	a.buildUI()
	return a
}

// scanFile does a one-shot batch read of cfg.Paths (base filters AND
// liveFilter applied), producing a fresh Aggregator/RingBuffer/Detector.
// Used for the initial seed, whenever the in-TUI filter changes, and
// (in static mode) that's the only data source there is. A large file
// can take several seconds to scan; ctx is checked periodically (not on
// every line, to keep the check itself cheap) so Ctrl+C during that
// wait aborts promptly instead of silently being ignored until the scan
// finishes on its own.
func (a *App) scanFile(ctx context.Context, liveFilter filter.And) (*stats.Aggregator, *RingBuffer, *anomaly.Detector) {
	agg := stats.NewAggregator(0, stats.AutoBucket, a.cfg.TrackCountry)
	if a.cfg.PathLabel != nil {
		agg.SetPathLabeler(a.cfg.PathLabel)
	}
	buf := NewRingBuffer(a.cfg.BufferSize)
	det := anomaly.NewDetector(a.cfg.Anomaly)

	rc, err := input.Open(a.cfg.Paths)
	if err != nil {
		return agg, buf, det
	}
	defer rc.Close()

	sc := input.NewLineScanner(rc)
	for i := 0; sc.Scan(); i++ {
		if i%4096 == 0 && ctx.Err() != nil {
			return agg, buf, det
		}
		line := sc.Text()
		if line == "" {
			continue
		}
		rec, perr := a.cfg.Parser.Parse(line)
		if perr != nil {
			continue
		}
		if !a.cfg.BaseFilters.Match(rec) || !liveFilter.Match(rec) {
			continue
		}
		agg.Add(rec)
		buf.Add(Entry{Record: rec, Raw: line})
		det.Observe(rec.RemoteAddr(), rec.Path(), recordTimeOrNow(rec))
	}
	return agg, buf, det
}

func recordTimeOrNow(rec *record.Record) time.Time {
	if t, ok := rec.Time(); ok {
		return t
	}
	return time.Now()
}

func (a *App) buildUI() {
	a.header = newHeader()
	a.footer = newFooter()

	a.views = a.buildViews()
	a.pages = tview.NewPages()
	for _, v := range a.views {
		a.pages.AddPage(v.id, v.primitive, true, false)
	}
	a.activeView = 0
	a.pages.SwitchToPage(a.views[0].id)
	a.renderActiveView()

	a.rootFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.footer, 2, 0, false)

	a.app.SetInputCapture(a.handleGlobalKey)
	a.app.SetRoot(a.rootFlex, true)
	a.refreshHeaderFooter("")
}

func (a *App) buildViews() []*viewDef {
	var views []*viewDef

	addTable := func(id, title string, key rune, dimension string) {
		v := &viewDef{id: id, title: title, key: key, dimension: dimension, table: newCountTable()}
		v.primitive = v.table
		v.table.SetSelectedFunc(func(row, col int) { a.onEnter(v, row) })
		views = append(views, v)
	}

	addTable("status", "status", '1', "status")
	addTable("ips", "ips", '2', "ip")
	if a.cfg.TrackCountry {
		addTable("countries", "countries", '3', "country")
	}
	addTable("paths", "paths", '4', "path")
	if a.cfg.ShowAgents {
		addTable("agents", "agents", '5', "user_agent")
	}
	if a.cfg.ShowReferers {
		addTable("referers", "referers", '6', "referer")
	}

	timeline := &viewDef{id: "timeline", title: "timeline", key: '7'}
	timeline.text = tview.NewTextView().SetDynamicColors(true)
	timeline.primitive = timeline.text
	views = append(views, timeline)

	if a.cfg.PathLabel != nil {
		routesView := &viewDef{id: "routes", title: "routes", key: '8', dimension: "route"}
		routesView.table = newCountTable()
		routesView.primitive = routesView.table
		routesView.table.SetSelectedFunc(func(row, col int) { a.onEnter(routesView, row) })
		views = append(views, routesView)
	}

	raw := &viewDef{id: "raw", title: "raw", key: 'l'}
	raw.text = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	raw.primitive = raw.text
	views = append(views, raw)

	alerts := &viewDef{id: "alerts", title: "alerts", key: 'a'}
	alerts.table = newCountTable()
	alerts.primitive = alerts.table
	alerts.table.SetSelectedFunc(func(row, col int) { a.onAlertEnter(row) })
	views = append(views, alerts)

	return views
}

// Run starts the tailing/tick goroutines (Live only) and blocks running
// the TUI event loop until the user quits or ctx is cancelled. In
// static mode (Config.Live == false), neither goroutine starts at all:
// there's nothing to tail and nothing that changes between ticks, so
// running them would just be wasted background work.
func (a *App) Run(ctx context.Context) error {
	// NewApp's seed scan (before Run is ever called) can take a while on
	// a large file; if the context was already cancelled during that
	// wait (e.g. the user hit Ctrl+C while it was loading), don't start
	// anything - tcell/tview's Stop() assumes Run() got far enough to
	// initialize the screen, and calling it on an uninitialized screen
	// panics rather than no-oping.
	if err := ctx.Err(); err != nil {
		return err
	}

	if a.cfg.Live {
		go a.followLoop(ctx)
		go a.tickLoop(ctx)
	}
	go func() {
		<-ctx.Done()
		// Best-effort: if Run() below never got far enough to finish
		// setting up the screen (e.g. it's about to fail with "no such
		// device" in an environment with no controlling terminal),
		// Stop() can still panic on partially-initialized tcell state.
		// That's not a recoverable *application* error - swallow it so
		// a race on shutdown can't take down the whole process.
		defer func() { recover() }()
		a.app.Stop()
	}()

	return a.app.Run()
}

func (a *App) followLoop(ctx context.Context) {
	if len(a.cfg.Paths) != 1 {
		return // defensive: Run only starts this when Live, which main.go guarantees means exactly one path
	}
	_ = input.Follow(ctx, a.cfg.Paths[0], a.cfg.PollInterval, func(line string) error {
		a.ingestLive(line)
		return nil
	})
}

func (a *App) ingestLive(line string) {
	rec, err := a.cfg.Parser.Parse(line)
	if err != nil {
		return
	}

	a.mu.Lock()
	match := a.cfg.BaseFilters.Match(rec) && a.liveFilter.Match(rec)
	if match {
		a.agg.Add(rec)
		a.buf.Add(Entry{Record: rec, Raw: line})
		a.detector.Observe(rec.RemoteAddr(), rec.Path(), recordTimeOrNow(rec))
	}
	a.mu.Unlock()
}

func (a *App) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.Refresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.mu.Lock()
			paused := a.paused
			a.mu.Unlock()
			if paused {
				continue
			}

			a.mu.Lock()
			rep := a.agg.Report()
			alerts := a.detector.Tick(now)
			a.mu.Unlock()

			rate := 0.0
			if !a.prevTickAt.IsZero() {
				if elapsed := now.Sub(a.prevTickAt).Seconds(); elapsed > 0 {
					rate = float64(rep.TotalRequests-a.prevTotal) / elapsed
				}
			}
			a.prevTotal = rep.TotalRequests
			a.prevTickAt = now

			a.app.QueueUpdateDraw(func() {
				a.lastReport = rep
				a.lastAlerts = alerts
				a.lastRate = rate
				a.renderActiveView()
				a.refreshHeaderFooter("")
			})
		}
	}
}

// renderActiveView must only be called from the main goroutine (a key
// handler) or from within app.QueueUpdateDraw.
func (a *App) renderActiveView() {
	v := a.views[a.activeView]
	rep := a.lastReport
	if rep == nil {
		return
	}
	switch v.id {
	case "status":
		renderStatusTable(v, rep.StatusDist, rep.TotalRequests)
	case "ips":
		renderCountTable(v, rep.TopIPs, rep.TotalRequests, "IP")
	case "countries":
		renderCountTable(v, rep.TopCountries, rep.TotalRequests, "COUNTRY")
	case "paths":
		renderCountTable(v, rep.TopPaths, rep.TotalRequests, "PATH")
	case "routes":
		renderRouteTimingTable(v, rep.RouteTiming, rep.TotalRequests)
	case "agents":
		renderCountTable(v, rep.TopUserAgents, rep.TotalRequests, "USER AGENT")
	case "referers":
		renderCountTable(v, rep.TopReferers, rep.TotalRequests, "REFERER")
	case "timeline":
		renderTimeline(v.text, rep.Histogram)
	case "alerts":
		renderAlertsTable(v.table, a.lastAlerts)
	case "raw":
		a.mu.Lock()
		entries := a.buf.Entries()
		a.mu.Unlock()
		renderRaw(v.text, entries)
	}
}

func (a *App) refreshHeaderFooter(message string) {
	rep := a.lastReport
	if rep == nil {
		rep = &stats.Report{}
	}
	activeAlerts := 0
	for _, al := range a.lastAlerts {
		if al.Active {
			activeAlerts++
		}
	}
	a.mu.Lock()
	paused := a.paused
	a.mu.Unlock()
	renderHeader(a.header, a.cfg.Version, strings.Join(a.cfg.Paths, ", "), rep, activeAlerts, a.startedAt, a.lastRate, a.cfg.Live, paused, false)

	v := a.views[a.activeView]
	renderFooterHint(a.footer, v.title, a.liveFilterText, message)
	fmt.Fprint(a.footer, "\n"+a.hotkeyBar())
}

// hotkeyBar lists the actually-registered views' hotkeys (so it never
// advertises a key for a view that was hidden by --show-agents/
// --show-referers not being set, or that isn't present in static mode)
// plus the fixed action keys.
func (a *App) hotkeyBar() string {
	var sb strings.Builder
	sb.WriteString(" ")
	for _, v := range a.views {
		fmt.Fprintf(&sb, "[yellow]%s[-:-:-] %s  ", string(v.key), v.title)
	}
	sb.WriteString("[yellow]/[-:-:-] filter  [yellow]x[-:-:-] clear  ")
	if a.cfg.Live {
		sb.WriteString("[yellow]p[-:-:-] pause  ")
	}
	sb.WriteString("[yellow]Enter[-:-:-] detail  [yellow]Esc[-:-:-] back  [yellow]q[-:-:-] quit")
	return sb.String()
}

func (a *App) handleGlobalKey(event *tcell.EventKey) *tcell.EventKey {
	if a.filterFocused {
		return event
	}
	if event.Key() == tcell.KeyEscape {
		if a.pages.HasPage("detail") {
			a.closeDetail()
			return nil
		}
		return event
	}
	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case 'q':
			a.app.Stop()
			return nil
		case '/':
			a.openFilterBar()
			return nil
		case 'x':
			a.clearFilter()
			return nil
		case 'p':
			if a.cfg.Live {
				a.togglePause()
			}
			return nil
		case 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		default:
			for i, v := range a.views {
				if event.Rune() == v.key {
					a.switchToView(i)
					return nil
				}
			}
		}
	}
	return event
}

func (a *App) switchToView(i int) {
	if a.pages.HasPage("detail") {
		a.closeDetail()
	}
	a.activeView = i
	a.pages.SwitchToPage(a.views[i].id)
	a.renderActiveView()
	a.refreshHeaderFooter("")
}

// togglePause freezes/unfreezes the display: tickLoop stops
// recomputing and redrawing while paused, but ingestLive keeps feeding
// the live Aggregator/RingBuffer/Detector in the background regardless
// - nothing is missed, the screen just stops changing until resumed.
// On resume, refreshes immediately rather than waiting for the next
// tick, so there's no visible lag before the screen catches up.
func (a *App) togglePause() {
	a.mu.Lock()
	a.paused = !a.paused
	resumed := !a.paused
	a.mu.Unlock()

	if resumed {
		a.mu.Lock()
		rep := a.agg.Report()
		alerts := a.detector.Tick(time.Now())
		a.mu.Unlock()
		a.lastReport = rep
		a.lastAlerts = alerts
		a.renderActiveView()
	}
	a.refreshHeaderFooter("")
}

func (a *App) onEnter(v *viewDef, row int) {
	idx := row - 1
	if idx < 0 || idx >= len(v.entries) {
		return
	}
	a.showDetail(v.dimension, v.entries[idx].Key, v.entries[idx].Count)
}

func (a *App) onAlertEnter(row int) {
	idx := row - 1
	if idx < 0 || idx >= len(a.lastAlerts) {
		return
	}
	al := a.lastAlerts[idx]
	if dim := alertDimension(al.Type); dim != "" {
		// No all-time total to compare against for an alert (it's not
		// tied to one of the CountEntry views), so skip that note.
		a.showDetail(dim, al.Key, -1)
	}
}

// showDetail pushes a drill-down page for dimension=key. allTimeCount is
// the row's count from the live (unbounded) Aggregator if known, so the
// page can note when the ring buffer (bounded, recent-only) doesn't have
// all of it; pass -1 when there's no such total (see onAlertEnter).
func (a *App) showDetail(dimension, key string, allTimeCount int) {
	a.mu.Lock()
	all := a.buf.Entries()
	bufCap := a.buf.Cap()
	a.mu.Unlock()

	var matched []Entry
	for _, e := range all {
		if matchesDimension(e.Record, dimension, key, a.cfg.PathLabel) {
			matched = append(matched, e)
		}
	}
	page := buildDetailPage(dimension, key, bufCap, matched, a.cfg.TrackCountry, allTimeCount, a.cfg.PathLabel)
	a.pages.AddAndSwitchToPage("detail", page, true)
}

func (a *App) closeDetail() {
	a.pages.RemovePage("detail")
	a.pages.SwitchToPage(a.views[a.activeView].id)
}

func (a *App) openFilterBar() {
	a.filterFocused = true
	field := tview.NewInputField().
		SetLabel(" filter: ").
		SetText(a.liveFilterText).
		SetFieldWidth(0)
	field.SetDoneFunc(func(key tcell.Key) {
		a.filterFocused = false
		switch key {
		case tcell.KeyEnter:
			a.applyFilter(field.GetText())
		case tcell.KeyEscape:
			// leave the current filter as-is
		}
		a.rootFlex.RemoveItem(field)
		a.rootFlex.AddItem(a.footer, 2, 0, false)
		a.app.SetFocus(a.pages)
		a.refreshHeaderFooter("")
	})
	a.rootFlex.RemoveItem(a.footer)
	a.rootFlex.AddItem(field, 1, 0, true)
	a.app.SetFocus(field)
}

func (a *App) applyFilter(expr string) {
	newFilter, err := ParseFilterExpr(expr)
	if err != nil {
		a.refreshHeaderFooter(fmt.Sprintf("filter error: %v", err))
		return
	}
	a.liveFilterText = expr
	a.reseed(newFilter)
}

func (a *App) clearFilter() {
	a.liveFilterText = ""
	a.reseed(filter.And{})
}

// reseed re-scans the file with a new in-TUI filter, replacing the live
// Aggregator/RingBuffer/Detector. Runs the (potentially slow, for large
// files) scan on a background goroutine so the UI doesn't freeze.
func (a *App) reseed(newFilter filter.And) {
	a.refreshHeaderFooter("reloading…")
	go func() {
		agg, buf, det := a.scanFile(a.ctx, newFilter)

		a.mu.Lock()
		a.agg, a.buf, a.detector = agg, buf, det
		a.liveFilter = newFilter
		// Report() must run while a.mu is still held: agg is now also
		// a.agg, and ingestLive's a.agg.Add() (called under the same
		// lock from the tail goroutine) could otherwise run
		// concurrently with Report()'s map iteration - a real
		// "concurrent map read and map write" crash found in
		// production, not a hypothetical one.
		rep := agg.Report()
		a.mu.Unlock()

		a.app.QueueUpdateDraw(func() {
			a.lastReport = rep
			a.lastAlerts = nil
			a.renderActiveView()
			a.refreshHeaderFooter("")
		})
	}()
}

func renderRaw(tv *tview.TextView, entries []Entry) {
	tv.Clear()
	for _, e := range entries {
		fmt.Fprintln(tv, e.Raw)
	}
	tv.ScrollToEnd()
}
