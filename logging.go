package main

import (
	"fmt"
	"os"
)

const (
	ansiRed    = "\033[31;1m"
	ansiYellow = "\033[33;1m"
	ansiReset  = "\033[0m"
)

// useColor is decided once at startup: only colorize when stderr is a
// terminal and the user hasn't opted out via NO_COLOR (https://no-color.org).
var useColor = os.Getenv("NO_COLOR") == "" && isTerminal(os.Stderr)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func colorize(color, label string) string {
	if !useColor {
		return label
	}
	return color + label + ansiReset
}

// warnf prints a yellow "warning:"-prefixed message to stderr.
func warnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorize(ansiYellow, "warning:")+" "+format+"\n", args...)
}

// fatalf prints a red "error:"-prefixed message to stderr and exits(1).
func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorize(ansiRed, "error:")+" "+format+"\n", args...)
	os.Exit(1)
}
