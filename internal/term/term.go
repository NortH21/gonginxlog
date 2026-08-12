// Package term has the small amount of terminal-detection/ANSI-color
// logic shared by the CLI's own warn/error output (see main.go's
// logging.go) and the batch report's colorized text tables
// (internal/output), so the two don't duplicate the same TTY/NO_COLOR
// checks.
package term

import "os"

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
