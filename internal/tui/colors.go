package tui

import "github.com/gdamore/tcell/v2"

// statusColor maps an HTTP status code to the conventional
// curl/httpie/k9s-style semantic color.
func statusColor(code int) tcell.Color {
	switch {
	case code >= 500:
		return tcell.ColorRed
	case code >= 400:
		return tcell.ColorYellow
	case code >= 300:
		return tcell.ColorTeal
	case code >= 200:
		return tcell.ColorGreen
	default:
		return tcell.ColorWhite
	}
}

// statusColorTag is the tview dynamic-color tag form of statusColor, for
// use in TextView content (e.g. "[red]500[white]").
func statusColorTag(code int) string {
	switch {
	case code >= 500:
		return "red"
	case code >= 400:
		return "yellow"
	case code >= 300:
		return "teal"
	case code >= 200:
		return "green"
	default:
		return "white"
	}
}

func alertColor(active bool) tcell.Color {
	if active {
		return tcell.ColorRed
	}
	return tcell.ColorGray
}
