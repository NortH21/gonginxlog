package main

import (
	"fmt"
	"os"

	"github.com/north21/gonginxlog/internal/term"
)

// useColor is decided once at startup: only colorize when stderr is a
// terminal and the user hasn't opted out via NO_COLOR (https://no-color.org).
var useColor = term.UseColor(os.Stderr)

func colorize(color, label string) string {
	return term.Colorize(useColor, color, label)
}

// warnf prints a yellow "warning:"-prefixed message to stderr.
func warnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorize(term.Yellow, "warning:")+" "+format+"\n", args...)
}

// fatalf prints a red "error:"-prefixed message to stderr and exits(1).
func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorize(term.Red, "error:")+" "+format+"\n", args...)
	os.Exit(1)
}
