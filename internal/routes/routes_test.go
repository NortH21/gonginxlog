package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRoutesFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "routes.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write routes file: %v", err)
	}
	return p
}

func TestLoadAndLabelFirstMatchWins(t *testing.T) {
	p := writeRoutesFile(t, `
routes:
  - pattern: '^/gamecenter/game/'
    label: game_page
  - pattern: '^/counter'
    label: counter
  - pattern: '^/'
    label: other
`)
	rules, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	label, ok := rules.Label("/gamecenter/game/123")
	if !ok || label != "game_page" {
		t.Fatalf("expected game_page, got %q ok=%v", label, ok)
	}

	// Matches both rule 2 (counter) and rule 3 (catch-all /) - first
	// match in file order must win.
	label, ok = rules.Label("/counter?id=1")
	if !ok || label != "counter" {
		t.Fatalf("expected counter (first match), got %q ok=%v", label, ok)
	}
}

func TestLabelNoMatch(t *testing.T) {
	p := writeRoutesFile(t, `
routes:
  - pattern: '^/api/'
    label: api
`)
	rules, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := rules.Label("/static/app.js"); ok {
		t.Fatalf("expected no match for /static/app.js")
	}
}

func TestLoadInvalidRegexFailsFast(t *testing.T) {
	p := writeRoutesFile(t, `
routes:
  - pattern: '('
    label: broken
`)
	if _, err := Load(p); err == nil {
		t.Fatalf("expected an error for an invalid regexp")
	}
}

func TestLoadMissingLabelFails(t *testing.T) {
	p := writeRoutesFile(t, `
routes:
  - pattern: '^/api/'
`)
	if _, err := Load(p); err == nil {
		t.Fatalf("expected an error for a rule with no label")
	}
}

func TestLoadEmptyRoutesFails(t *testing.T) {
	p := writeRoutesFile(t, `routes: []`)
	if _, err := Load(p); err == nil {
		t.Fatalf("expected an error for an empty routes list")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/routes.yaml"); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
