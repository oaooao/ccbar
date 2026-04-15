package main

import (
	"fmt"
	"strings"
	"time"
)

// ANSI color constants.
const (
	reset      = "\x1b[0m"
	bold       = "\x1b[1m"
	white      = "\x1b[37m"
	boldWhite  = "\x1b[1;37m"
	cyan       = "\x1b[36m"
	green      = "\x1b[32m"
	yellow     = "\x1b[33m"
	red        = "\x1b[31m"
	boldRed    = "\x1b[1;31m"
	boldYellow = "\x1b[1;33m"
	magenta    = "\x1b[38;5;141m"
	brightBlue = "\x1b[38;5;75m"

	text      = "\x1b[38;5;252m"
	secondary = "\x1b[38;5;246m"
	muted     = "\x1b[38;5;243m"
	barEmpty  = "\x1b[38;5;242m"
)

const (
	sep     = " " + muted + "│" + reset + " "
	cfgSep  = " " + muted + "·" + reset + " "
	labelW  = 7
	ctxBarW = 40
	rateBarW = 30
)

// contextBar renders a context window progress bar.
func contextBar(pct, width int) string {
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	empty := width - filled

	var color string
	switch {
	case pct >= 80:
		color = red
	case pct >= 60:
		color = yellow
	default:
		color = cyan
	}

	return color + strings.Repeat("━", filled) + barEmpty + strings.Repeat("━", empty) + reset
}

// rateBar renders a rate limit progress bar.
func rateBar(pct, width int) string {
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	empty := width - filled

	var filledColor string
	switch {
	case pct >= 80:
		filledColor = red
	case pct >= 60:
		filledColor = yellow
	default:
		filledColor = brightBlue
	}

	return filledColor + strings.Repeat("▰", filled) + barEmpty + strings.Repeat("▱", empty) + reset
}

// pctColor returns the color for a percentage value.
func pctColor(pct int) string {
	switch {
	case pct >= 80:
		return boldRed
	case pct >= 60:
		return yellow
	default:
		return text
	}
}

// padPct formats a percentage right-aligned to 4 characters.
func padPct(pct int) string {
	return fmt.Sprintf("%3d%%", pct)
}

// label pads a string to labelW characters.
func label(s string) string {
	if len(s) >= labelW {
		return text + s + reset
	}
	return text + s + strings.Repeat(" ", labelW-len(s)) + reset
}

// formatDuration formats milliseconds into a human-readable duration.
func formatDuration(ms float64) string {
	totalSec := int(ms / 1000)
	if totalSec < 60 {
		return fmt.Sprintf("%ds", totalSec)
	}
	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// formatResetTime formats a time as "1:00pm".
func formatResetTime(t time.Time) string {
	h := t.Hour()
	m := t.Minute()
	ampm := "am"
	if h >= 12 {
		ampm = "pm"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d%s", h12, m, ampm)
}

// formatResetDateTime formats a time as "Apr 18, 4:00am".
func formatResetDateTime(t time.Time) string {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	h := t.Hour()
	m := t.Minute()
	ampm := "am"
	if h >= 12 {
		ampm = "pm"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%s %d, %d:%02d%s", months[t.Month()-1], t.Day(), h12, m, ampm)
}
