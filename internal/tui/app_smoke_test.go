package tui

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/parser"
)

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
		Path:         logPath,
		Parser:       p,
		TrackCountry: true,
		BufferSize:   100,
		Refresh:      30 * time.Millisecond,
		PollInterval: 30 * time.Millisecond,
	})

	if app.lastReport == nil || app.lastReport.TotalRequests != 20 {
		t.Fatalf("expected the seed scan to have counted 20 requests, got %+v", app.lastReport)
	}

	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	app.app.SetScreen(screen)

	runErrCh := make(chan error, 1)
	go func() { runErrCh <- app.Run(ctx) }()
	time.Sleep(150 * time.Millisecond) // let the first tick/render happen

	if got := app.views[app.activeView].id; got != "status" {
		t.Fatalf("expected the default view to be 'status', got %q", got)
	}

	screen.InjectKey(tcell.KeyRune, '2', tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if got := app.views[app.activeView].id; got != "ips" {
		t.Fatalf("expected view 'ips' after pressing '2', got %q", got)
	}
	if len(app.views[app.activeView].entries) == 0 {
		t.Fatalf("expected the ips view to have entries after the seed scan")
	}

	// Enter should drill down into the top row.
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if !app.pages.HasPage("detail") {
		t.Fatalf("expected a 'detail' page to be pushed after Enter")
	}

	screen.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	time.Sleep(80 * time.Millisecond)
	if app.pages.HasPage("detail") {
		t.Fatalf("expected Esc to close the detail page")
	}

	// "/" opens the filter bar; type "status:404" and Enter should
	// trigger a reseed that narrows the live report down to the 5
	// seeded 404s.
	screen.InjectKey(tcell.KeyRune, '/', tcell.ModNone)
	time.Sleep(30 * time.Millisecond)
	if !app.filterFocused {
		t.Fatalf("expected the filter bar to be focused after '/'")
	}
	for _, r := range "status:404" {
		screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
	}
	screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(150 * time.Millisecond) // reseed runs on a goroutine

	if app.liveFilterText != "status:404" {
		t.Fatalf("expected liveFilterText to be %q, got %q", "status:404", app.liveFilterText)
	}
	if app.lastReport == nil || app.lastReport.TotalRequests != 5 {
		t.Fatalf("expected the status:404 filter to narrow the report to 5 requests, got %+v", app.lastReport)
	}

	// "x" clears back to the full set.
	screen.InjectKey(tcell.KeyRune, 'x', tcell.ModNone)
	time.Sleep(150 * time.Millisecond)
	if app.liveFilterText != "" {
		t.Fatalf("expected liveFilterText to be cleared, got %q", app.liveFilterText)
	}
	if app.lastReport == nil || app.lastReport.TotalRequests != 20 {
		t.Fatalf("expected clearing the filter to restore all 20 requests, got %+v", app.lastReport)
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

func writeTempLog(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "access-*.log")
	if err != nil {
		t.Fatalf("create temp log: %v", err)
	}
	defer f.Close()

	line := func(status string) string {
		return `{"remote_addr":"203.0.113.5","country":"US","remote_user":"",` +
			`"time_local":"12/Aug/2026:08:00:00 +0000","ssl_protocol":"","host":"example.com",` +
			`"request":"GET /foo HTTP/1.1","status":"` + status + `","bytes_sent":"100","http_referer":"-",` +
			`"http_user_agent":"curl/8.0","http_cookie":"-","http_x_forwarded_for":"-",` +
			`"request_time":"0.01","request_length":"10","upstream_addr":"-",` +
			`"upstream_response_time":"-","upstream_status":"-","upstream_cache_status":"-",` +
			`"x-request-id":"1"}` + "\n"
	}
	// 15 x 200 + 5 x 404, so the smoke test can exercise --status:404
	// narrowing the live report to a known count.
	for i := 0; i < 15; i++ {
		if _, err := f.WriteString(line("200")); err != nil {
			t.Fatalf("write temp log: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := f.WriteString(line("404")); err != nil {
			t.Fatalf("write temp log: %v", err)
		}
	}
	return f.Name()
}
