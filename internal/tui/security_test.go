package tui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/north21/gonginxlog/internal/record"
)

// This file covers the fix for a real class of bug: every string this
// package displays that ultimately traces back to a log field (a path,
// User-Agent, referer, or the raw line) is fully attacker-controlled -
// nginx logs an HTTP request's fields verbatim. Printed unsanitized
// into a tview.TextView with SetDynamicColors(true), or into a
// tview.Table cell (which interprets "[tag]" markup unconditionally,
// with no toggle to disable it - see rivo/tview's printWithStyle), a
// crafted request could inject terminal escape sequences or forge
// tview's own color/region markup. See internal/term.Sanitize and
// safeCellText/tview.Escape.

func TestRenderRawSanitizesControlBytesAndEscapesMarkup(t *testing.T) {
	tv := tview.NewTextView().SetDynamicColors(true)
	malicious := "GET /evil\x1b[31mHTTP/1.1\" \"[red]fake-color[-:-:-]"
	renderRaw(tv, []Entry{{Raw: malicious}})

	stored := tv.GetText(false)
	if strings.ContainsRune(stored, 0x1b) {
		t.Fatalf("expected no raw ESC byte in the rendered raw view, got %q", stored)
	}
	if strings.Contains(stored, "[red]fake-color[-:-:-]") {
		t.Fatalf("expected tview markup tags to be escaped, got %q", stored)
	}
	// tview.Escape's signature transformation: "[tag]" becomes "[tag[]"
	// so it's rendered as literal text instead of being interpreted.
	if !strings.Contains(stored, "[red[]") {
		t.Fatalf("expected the escaped-tag marker in the output, got %q", stored)
	}
}

func TestSafeCellTextStripsControlBytesAndEscapesMarkup(t *testing.T) {
	got := safeCellText("bot\x1b]0;pwned\x07 [green]ok[-:-:-]")
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("expected no control bytes, got %q", got)
	}
	if strings.Contains(got, "[green]ok[-:-:-]") {
		t.Fatalf("expected tview markup to be escaped, got %q", got)
	}
}

func TestSafeCellTextLeavesOrdinaryValuesReadable(t *testing.T) {
	if got := safeCellText("/gamecenter/game/1"); got != "/gamecenter/game/1" {
		t.Fatalf("safeCellText changed an ordinary value: got %q", got)
	}
}

func TestBuildDetailPageEscapesKeyInHeaderAndTitle(t *testing.T) {
	maliciousKey := "evil-ua[red]injected[-:-:-]\x1b[0m"
	rec := &record.Record{Fields: map[string]string{
		"remote_addr":     "203.0.113.5",
		"http_user_agent": maliciousKey,
	}}
	entries := []Entry{{Record: rec, Raw: "raw line"}}

	page := buildDetailPage("user_agent", maliciousKey, 100, entries, false, -1, nil, false)

	flex, ok := page.(*tview.Flex)
	if !ok {
		t.Fatalf("expected buildDetailPage to return a *tview.Flex, got %T", page)
	}

	title := flex.GetTitle()
	if strings.ContainsRune(title, 0x1b) {
		t.Fatalf("expected no raw ESC byte in the detail page title, got %q", title)
	}
	if strings.Contains(title, "[red]injected[-:-:-]") {
		t.Fatalf("expected the key's tview markup to be escaped in the title, got %q", title)
	}

	header, ok := flex.GetItem(0).(*tview.TextView)
	if !ok {
		t.Fatalf("expected the first flex item to be the header *tview.TextView, got %T", flex.GetItem(0))
	}
	headerText := header.GetText(false)
	if strings.ContainsRune(headerText, 0x1b) {
		t.Fatalf("expected no raw ESC byte in the detail page header, got %q", headerText)
	}
	if strings.Contains(headerText, "[red]injected[-:-:-]") {
		t.Fatalf("expected the key's tview markup to be escaped in the header, got %q", headerText)
	}
}
