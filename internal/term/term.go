// Package term has the small amount of terminal-detection/ANSI-color
// logic shared by the CLI's own warn/error output (see main.go's
// logging.go) and the batch report's colorized text tables
// (internal/output), so the two don't duplicate the same TTY/NO_COLOR
// checks.
package term

import (
	"os"
	"strings"
)

// IsTerminal reports whether f is a character device (a real terminal),
// as opposed to a pipe, redirected file, or similar.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// UseColor reports whether ANSI colors should be used when writing to f:
// only when f is a real terminal and the user hasn't opted out via
// NO_COLOR (https://no-color.org).
func UseColor(f *os.File) bool {
	return os.Getenv("NO_COLOR") == "" && IsTerminal(f)
}

// Standard ANSI color codes used across the CLI's colored output.
const (
	Red    = "\033[31;1m"
	Yellow = "\033[33;1m"
	Green  = "\033[32;1m"
	Cyan   = "\033[36;1m"
	Reset  = "\033[0m"
)

// Colorize wraps label in color when enabled is true; returns label
// unchanged otherwise.
func Colorize(enabled bool, color, label string) string {
	if !enabled {
		return label
	}
	return color + label + Reset
}

// Sanitize strips ASCII/Unicode control characters (C0 0x00-0x1F, DEL
// 0x7F, C1 0x80-0x9F) from s. Every string this CLI prints that
// originates from a log field (request path, User-Agent, referer, raw
// line, ...) came from an HTTP request an attacker fully controls -
// nginx logs it verbatim, and none of those bytes have a legitimate
// reason to include a control character. Without stripping them, a
// crafted request could inject terminal escape sequences (forging or
// hiding output) into whatever displays the log later. Called
// unconditionally, not just when writing to a real terminal, since
// output can still be piped into something that interprets escapes
// downstream.
func Sanitize(s string) string {
	dirty := false
	for _, r := range s {
		if isControlRune(r) {
			dirty = true
			break
		}
	}
	if !dirty {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControlRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isControlRune(r rune) bool {
	return r == 0x7f || (r >= 0x00 && r <= 0x1f) || (r >= 0x80 && r <= 0x9f)
}
