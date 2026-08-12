package term

import (
	"strings"
	"testing"
)

func TestSanitizeLeavesCleanStringsUntouched(t *testing.T) {
	clean := "GET /foo/bar?x=1 HTTP/1.1 日本語"
	if got := Sanitize(clean); got != clean {
		t.Fatalf("Sanitize(%q) = %q, want unchanged", clean, got)
	}
}

func TestSanitizeStripsTab(t *testing.T) {
	if got := Sanitize("a\tb"); got != "ab" {
		t.Fatalf("Sanitize with tab = %q, want %q", got, "ab")
	}
}

func TestSanitizeStripsC0ControlChars(t *testing.T) {
	// \x1b is the ESC byte that starts an ANSI/VT100 escape sequence - the
	// exact class of thing this function exists to neutralize, since it
	// can appear in attacker-controlled log fields (e.g. a crafted
	// User-Agent) that nginx logs verbatim.
	in := "evil\x1b[31mred\x1b[0m and \x07bell and \x00nul"
	got := Sanitize(in)
	if strings.ContainsAny(got, "\x1b\x07\x00") {
		t.Fatalf("Sanitize(%q) = %q, still contains a control byte", in, got)
	}
	want := "evil[31mred[0m and bell and nul"
	if got != want {
		t.Fatalf("Sanitize(%q) = %q, want %q", in, got, want)
	}
}

func TestSanitizeStripsDEL(t *testing.T) {
	if got := Sanitize("a\x7fb"); got != "ab" {
		t.Fatalf("Sanitize with DEL = %q, want %q", got, "ab")
	}
}

func TestSanitizeStripsC1ControlChars(t *testing.T) {
	// C1 controls (U+0080-U+009F) include CSI (U+009B) - some terminals
	// honor 8-bit control sequences directly, not just the ESC-prefixed
	// 7-bit form. json.Decode produces exactly this rune from a
	// \u009b escape in a JSON-format log field.
	in := "a\u009bb"
	if got := Sanitize(in); got != "ab" {
		t.Fatalf("Sanitize(%q) = %q, want %q", in, got, "ab")
	}
}

func TestSanitizeStripsEmbeddedNewlinesAndCarriageReturns(t *testing.T) {
	// A decoded JSON log field can legitimately contain a literal
	// newline/CR (nginx's escape=json encodes it as \n in the file, but
	// json.Decode turns that back into a real newline byte) - printed
	// unsanitized into a single table row or line, that forges extra
	// output the reader didn't ask for.
	in := "line one\r\nline two"
	got := Sanitize(in)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("Sanitize(%q) = %q, still contains a newline/CR", in, got)
	}
}
