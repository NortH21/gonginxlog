package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/parser"
)

// syncRead runs fn on the app's own tview event goroutine (the same one
// that owns lastReport/activeView/etc.) and returns its result, so tests
// can inspect App state without racing the ticker/key-handler goroutines
// that mutate it - a straight `app.lastReport` read from the test
// goroutine would be a real, if test-only, data race once app.Run has
// started.
func syncRead[T any](app *App, fn func() T) T {
	ch := make(chan T, 1)
	app.app.QueueUpdateDraw(func() { ch <- fn() })
	return <-ch
}

// TestAppSmoke drives the App headlessly via a tcell SimulationScreen -
// there's no real TTY in CI/this sandbox, so this is how the app's key
// handling and view switching get verified without a human watching a
// terminal (see DESIGN.md's verification notes).
func TestAppSmoke(t *testing.T) {
	logPath := writeTempLog(t)

	spec := format.Default()
	p, err := parser.New(spec)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(ctx, Config{
		Paths:        []string{logPath},
		Live:         true,
		Parser:       p,
		TrackCountry: true,
		BufferSize:   100,
		Refresh:      30 * time.Millisecond,
		PollInterval: 30 * time.Millisecond,
	})

	// Safe to read directly: no goroutines are running yet at this point.
	if app.lastReport == nil || app.lastReport.TotalRequests != 20 {
		t.Fatalf("expected the seed scan to have counted 20 requests, got %+v", app.lastReport)
	}

	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	app.app.SetScreen(screen)

	runErrCh := make(chan error, 1)
	go func() { runErrCh <- app.Run(ctx) }()
	time.Sleep(150 * time.Millisecond) // let the first tick/render happen

	if got := syncRead(app, func() string { return app.views[app.activeView].id }); got != "status" {
		t.Fatalf("expected the default view to be 'status', got %q", got)
	}

	screen.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if got := syncRead(app, func() string { return app.views[app.activeView].id }); got != "ips" {
		t.Fatalf("expected view 'ips' after pressing '2', got %q", got)
	}
	if n := syncRead(app, func() int { return len(app.views[app.activeView].entries) }); n == 0 {
		t.Fatalf("expected the ips view to have entries after the seed scan")
	}

	// Enter should drill down into the top row.
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if !syncRead(app, func() bool { return app.pages.HasPage("detail") }) {
		t.Fatalf("expected a 'detail' page to be pushed after Enter")
	}

	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if syncRead(app, func() bool { return app.pages.HasPage("detail") }) {
		t.Fatalf("expected Esc to close the detail page")
	}

	// "/" opens the filter bar; type "status:404" and Enter should
	// trigger a reseed that narrows the live report down to the 5
	// seeded 404s.
	screen.InjectKey(tcell.KeyRune, '/', tcell.ModNone)
	time.Sleep(30 * time.Millisecond)
	if !syncRead(app, func() bool { return app.filterFocused }) {
		t.Fatalf("expected the filter bar to be focused after '/'")
	}
	for _, r := range "status:404" {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(150 * time.Millisecond) // reseed runs on a goroutine

	if got := syncRead(app, func() string { return app.liveFilterText }); got != "status:404" {
		t.Fatalf("expected liveFilterText to be %q, got %q", "status:404", got)
	}
	if n := syncRead(app, func() int { return app.lastReport.TotalRequests }); n != 5 {
		t.Fatalf("expected the status:404 filter to narrow the report to 5 requests, got %d", n)
	}

	// "x" clears back to the full set.
	screen.InjectKey(tcell.KeyRune, 'x', tcell.ModNone)
	time.Sleep(150 * time.Millisecond)
	if got := syncRead(app, func() string { return app.liveFilterText }); got != "" {
		t.Fatalf("expected liveFilterText to be cleared, got %q", got)
	}
	if n := syncRead(app, func() int { return app.lastReport.TotalRequests }); n != 20 {
		t.Fatalf("expected clearing the filter to restore all 20 requests, got %d", n)
	}

	// "p" pauses: new appended lines must still be ingested in the
	// background, but lastReport must not change until resumed.
	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)
	time.Sleep(30 * time.Millisecond)
	if !syncRead(app, func() bool { return app.paused }) {
		t.Fatalf("expected paused=true after pressing 'p'")
	}
	appendLine(t, logPath, "200")
	time.Sleep(150 * time.Millisecond) // long enough for several ticks, if unpaused
	if n := syncRead(app, func() int { return app.lastReport.TotalRequests }); n != 20 {
		t.Fatalf("expected lastReport to stay at 20 while paused, got %d", n)
	}

	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if syncRead(app, func() bool { return app.paused }) {
		t.Fatalf("expected paused=false after pressing 'p' again")
	}
	if n := syncRead(app, func() int { return app.lastReport.TotalRequests }); n != 21 {
		t.Fatalf("expected lastReport to catch up to 21 after resuming, got %d", n)
	}

	screen.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)
	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("app.Run returned an error: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("app did not quit within the timeout after 'q'")
	}
}

// TestAppSmokeStatic verifies static mode (Config.Live == false):
// multiple files load fine, the header says STATIC, no follow/tick
// goroutine runs (a line appended after Run() must NOT show up in
// lastReport - proving nothing is tailing), filter changes still work
// via reseed, and quitting is still clean.
func TestAppSmokeStatic(t *testing.T) {
	pathA := writeTempLog(t)
	pathB := writeTempLog(t) // a second file: multi-file static must work

	spec := format.Default()
	p, err := parser.New(spec)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(ctx, Config{
		Paths:      []string{pathA, pathB},
		Live:       false,
		Parser:     p,
		BufferSize: 100,
		Refresh:    30 * time.Millisecond, // irrelevant in static mode - tickLoop never starts
	})

	if app.lastReport == nil || app.lastReport.TotalRequests != 40 {
		t.Fatalf("expected 40 requests (20 per file x2 files), got %+v", app.lastReport)
	}

	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	app.app.SetScreen(screen)

	runErrCh := make(chan error, 1)
	go func() { runErrCh <- app.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)

	if text := syncRead(app, func() string { return app.header.GetText(true) }); !strings.Contains(text, "STATIC") {
		t.Fatalf("expected the header to show STATIC, got %q", text)
	}

	// Nothing should be tailing: appending to pathA must not change
	// lastReport, even after waiting well past a normal tick interval.
	appendLine(t, pathA, "200")
	time.Sleep(150 * time.Millisecond)
	if n := syncRead(app, func() int { return app.lastReport.TotalRequests }); n != 40 {
		t.Fatalf("expected lastReport to stay at 40 in static mode (nothing tails), got %d", n)
	}

	// Filter changes still work via reseed, independent of the tick loop.
	screen.InjectKey(tcell.KeyRune, '/', tcell.ModNone)
	time.Sleep(30 * time.Millisecond)
	for _, r := range "status:404" {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(150 * time.Millisecond)
	if n := syncRead(app, func() int { return app.lastReport.TotalRequests }); n != 10 {
		t.Fatalf("expected the status:404 filter to narrow to 10 (5 per file x2), got %d", n)
	}

	// 'p' (pause) is a live-only concept and must be a no-op here.
	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)
	time.Sleep(30 * time.Millisecond)
	if syncRead(app, func() bool { return app.paused }) {
		t.Fatalf("expected 'p' to be a no-op in static mode")
	}

	screen.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)
	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("app.Run returned an error: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("app did not quit within the timeout after 'q'")
	}
}

// TestBuildViewsRespectsShowAgentsReferers guards --show-agents/
// --show-referers actually surfacing the agents/referers views (and
// their hotkeys) - a config wiring bug here would silently drop a view
// with no error, which is exactly what happened in practice (main.go
// built tui.Config correctly, but nothing had asserted the view list
// itself matched).
func TestBuildViewsRespectsShowAgentsReferers(t *testing.T) {
	logPath := writeTempLog(t)
	spec := format.Default()
	p, err := parser.New(spec)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(ctx, Config{
		Paths:        []string{logPath},
		Live:         false,
		Parser:       p,
		ShowAgents:   true,
		ShowReferers: true,
	})

	if !hasView(app, "agents", '5') {
		t.Fatalf("expected an 'agents' view with hotkey '5' when ShowAgents is true, got views %+v", viewIDs(app))
	}
	if !hasView(app, "referers", '6') {
		t.Fatalf("expected a 'referers' view with hotkey '6' when ShowReferers is true, got views %+v", viewIDs(app))
	}
}

func TestBuildViewsHideAgentsReferersByDefault(t *testing.T) {
	logPath := writeTempLog(t)
	spec := format.Default()
	p, err := parser.New(spec)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(ctx, Config{
		Paths:  []string{logPath},
		Live:   false,
		Parser: p,
	})

	if hasView(app, "agents", '5') {
		t.Fatalf("expected no 'agents' view when ShowAgents is false, got views %+v", viewIDs(app))
	}
	if hasView(app, "referers", '6') {
		t.Fatalf("expected no 'referers' view when ShowReferers is false, got views %+v", viewIDs(app))
	}
}

// TestBuildViewsRespectsTrackUpstream guards the "upstreams" view (and
// its hotkey '9') actually appearing when Config.TrackUpstream is set -
// same wiring-regression concern as ShowAgents/ShowReferers above.
func TestBuildViewsRespectsTrackUpstream(t *testing.T) {
	logPath := writeTempLog(t)
	spec := format.Default()
	p, err := parser.New(spec)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(ctx, Config{
		Paths:         []string{logPath},
		Live:          false,
		Parser:        p,
		TrackUpstream: true,
	})

	if !hasView(app, "upstreams", '9') {
		t.Fatalf("expected an 'upstreams' view with hotkey '9' when TrackUpstream is true, got views %+v", viewIDs(app))
	}
}

func TestBuildViewsHideUpstreamsByDefault(t *testing.T) {
	logPath := writeTempLog(t)
	spec := format.Default()
	p, err := parser.New(spec)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := NewApp(ctx, Config{
		Paths:  []string{logPath},
		Live:   false,
		Parser: p,
	})

	if hasView(app, "upstreams", '9') {
		t.Fatalf("expected no 'upstreams' view when TrackUpstream is false, got views %+v", viewIDs(app))
	}
}

func hasView(app *App, id string, key rune) bool {
	for _, v := range app.views {
		if v.id == id {
			return v.key == key
		}
	}
	return false
}

func viewIDs(app *App) []string {
	ids := make([]string, len(app.views))
	for i, v := range app.views {
		ids[i] = v.id
	}
	return ids
}

func logLine(status string) string {
	return `{"remote_addr":"203.0.113.5","country":"US","remote_user":"",` +
		`"time_local":"12/Aug/2026:08:00:00 +0000","ssl_protocol":"","host":"example.com",` +
		`"request":"GET /foo HTTP/1.1","status":"` + status + `","bytes_sent":"100","http_referer":"-",` +
		`"http_user_agent":"curl/8.0","http_cookie":"-","http_x_forwarded_for":"-",` +
		`"request_time":"0.01","request_length":"10","upstream_addr":"-",` +
		`"upstream_response_time":"-","upstream_status":"-","upstream_cache_status":"-",` +
		`"x-request-id":"1"}` + "\n"
}

func writeTempLog(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "access-*.log")
	if err != nil {
		t.Fatalf("create temp log: %v", err)
	}
	defer f.Close()

	// 15 x 200 + 5 x 404, so the smoke test can exercise --status:404
	// narrowing the live report to a known count.
	for i := 0; i < 15; i++ {
		if _, err := f.WriteString(logLine("200")); err != nil {
			t.Fatalf("write temp log: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := f.WriteString(logLine("404")); err != nil {
			t.Fatalf("write temp log: %v", err)
		}
	}
	return f.Name()
}

func appendLine(t *testing.T, path, status string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open temp log for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(logLine(status)); err != nil {
		t.Fatalf("append temp log: %v", err)
	}
}
