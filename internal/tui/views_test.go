package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/north21/gonginxlog/internal/stats"
)

func TestRenderUpstreamsTableRendersAddrTimingAndErrorRate(t *testing.T) {
	v := &viewDef{table: newCountTable()}
	entries := []stats.UpstreamEntry{
		{Addr: "10.0.0.1:8080", Count: 8, AvgSeconds: 0.042, TimedCount: 8, ErrorCount: 1},
		{Addr: "-", Count: 2, TimedCount: 0, ErrorCount: 0},
	}
	renderUpstreamsTable(v, entries, 10)

	if got := v.table.GetCell(1, 1).Text; got != "10.0.0.1:8080" {
		t.Fatalf("row 1 ADDR = %q, want %q", got, "10.0.0.1:8080")
	}
	if got := v.table.GetCell(1, 4).Text; got != "0.042s" {
		t.Fatalf("row 1 AVG = %q, want %q", got, "0.042s")
	}
	if got := v.table.GetCell(1, 5).Text; got != "12.5%" {
		t.Fatalf("row 1 ERR%% = %q, want %q (1/8)", got, "12.5%")
	}
	if color, _, _ := v.table.GetCell(1, 5).Style.Decompose(); color != tcell.ColorRed {
		t.Fatalf("expected the non-zero error rate to be colored red, got %v", color)
	}

	// The "-" (no upstream) row: AVG/ERR% are meaningless, shown as "-".
	if got := v.table.GetCell(2, 1).Text; got != "- (no upstream)" {
		t.Fatalf("row 2 ADDR = %q, want %q", got, "- (no upstream)")
	}
	if got := v.table.GetCell(2, 4).Text; got != "-" {
		t.Fatalf("row 2 AVG = %q, want %q", got, "-")
	}
	if got := v.table.GetCell(2, 5).Text; got != "-" {
		t.Fatalf("row 2 ERR%% = %q, want %q", got, "-")
	}
}

func TestRenderUpstreamsTablePopulatesEntriesForConsistency(t *testing.T) {
	v := &viewDef{table: newCountTable()}
	entries := []stats.UpstreamEntry{{Addr: "10.0.0.1:8080", Count: 5}}
	renderUpstreamsTable(v, entries, 5)

	if len(v.entries) != 1 || v.entries[0].Key != "10.0.0.1:8080" || v.entries[0].Count != 5 {
		t.Fatalf("expected v.entries to mirror the upstream entries, got %+v", v.entries)
	}
}
