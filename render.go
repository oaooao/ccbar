package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Theme holds all ANSI color codes for a specific background.
type Theme struct {
	Reset      string
	Bold       string
	White      string
	BoldWhite  string
	Cyan       string
	Green      string
	Yellow     string
	Red        string
	BoldRed    string
	BoldYellow string
	Magenta    string
	BrightBlue string
	Text       string
	Secondary  string
	Muted      string
	BarEmpty   string
	Label      string // gauge labels: context, 5h, weekly
}

var darkTheme = Theme{
	Reset:      "\x1b[0m",
	Bold:       "\x1b[1m",
	White:      "\x1b[37m",
	BoldWhite:  "\x1b[1;37m",
	Cyan:       "\x1b[36m",
	Green:      "\x1b[32m",
	Yellow:     "\x1b[33m",
	Red:        "\x1b[31m",
	BoldRed:    "\x1b[1;31m",
	BoldYellow: "\x1b[1;33m",
	Magenta:    "\x1b[38;5;141m",
	BrightBlue: "\x1b[38;5;75m",
	Text:       "\x1b[38;5;252m",
	Secondary:  "\x1b[38;5;246m",
	Muted:      "\x1b[38;5;243m",
	BarEmpty:   "\x1b[38;5;242m",
	Label:      "\x1b[38;5;252m",  // same as Text in dark theme
}

var lightTheme = Theme{
	Reset:      "\x1b[0m",
	Bold:       "\x1b[1m",
	White:      "\x1b[30m",          // black text on light bg
	BoldWhite:  "\x1b[1;30m",       // bold black
	Cyan:       "\x1b[38;5;30m",    // darker cyan
	Green:      "\x1b[38;5;28m",    // darker green
	Yellow:     "\x1b[38;5;130m",   // dark orange/amber
	Red:        "\x1b[38;5;160m",   // darker red
	BoldRed:    "\x1b[1;38;5;160m",
	BoldYellow: "\x1b[1;38;5;130m",
	Magenta:    "\x1b[38;5;90m",    // darker purple
	BrightBlue: "\x1b[38;5;26m",   // darker blue
	Text:       "\x1b[38;5;241m",   // percentage numbers — readable but lighter than labels
	Secondary:  "\x1b[38;5;244m",   // reset times — tertiary info
	Muted:      "\x1b[38;5;248m",   // config stats, separators
	BarEmpty:   "\x1b[38;5;253m",   // empty bar portions — subtle but visible
	Label:      "\x1b[38;5;244m",   // medium gray — visible but not heavy
}

// Active theme — set in main() based on --theme flag.
var th = darkTheme

const (
	labelW   = 7
	ctxBarW  = 40
	rateBarW = 30
)

// Computed separators (depend on theme, call after theme is set).
func sepStr() string    { return " " + th.Muted + "│" + th.Reset + " " }
func cfgSepStr() string { return " " + th.Muted + "·" + th.Reset + " " }

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
		color = th.Red
	case pct >= 60:
		color = th.Yellow
	default:
		color = th.Cyan
	}

	return color + strings.Repeat("━", filled) + th.BarEmpty + strings.Repeat("━", empty) + th.Reset
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
		filledColor = th.Red
	case pct >= 60:
		filledColor = th.Yellow
	default:
		filledColor = th.BrightBlue
	}

	return filledColor + strings.Repeat("▰", filled) + th.BarEmpty + strings.Repeat("▱", empty) + th.Reset
}

// pctColor returns the color for a percentage value.
func pctColor(pct int) string {
	switch {
	case pct >= 80:
		return th.BoldRed
	case pct >= 60:
		return th.Yellow
	default:
		return th.Text
	}
}

// padPct formats a percentage right-aligned to 4 characters.
func padPct(pct int) string {
	return fmt.Sprintf("%3d%%", pct)
}

// label pads a string to labelW characters with label-specific styling.
func label(s string) string {
	if len(s) >= labelW {
		return th.Label + s + th.Reset
	}
	return th.Label + s + strings.Repeat(" ", labelW-len(s)) + th.Reset
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

// ─── Locale-aware date/time formatting ───────────────────────────────────────

// localeOverride is set by --locale flag. Empty means auto-detect.
var localeOverride string

// isZhLocale checks if the active locale is Chinese.
func isZhLocale() bool {
	switch localeOverride {
	case "zh":
		return true
	case "en":
		return false
	}
	// Auto-detect from system
	for _, key := range []string{"LANG", "LC_TIME", "LC_ALL", "LANGUAGE"} {
		if v := os.Getenv(key); strings.HasPrefix(v, "zh") {
			return true
		}
	}
	return false
}

// formatResetTime formats a time for the 5h rate limit.
// zh: "15:00"   en: "3:00pm"
func formatResetTime(t time.Time) string {
	if isZhLocale() {
		return fmt.Sprintf("%d:%02d", t.Hour(), t.Minute())
	}
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

// formatResetDateTime formats a time for the 7d rate limit.
// zh: "4/18 15:00"   en: "Apr 18, 3:00pm"
func formatResetDateTime(t time.Time) string {
	if isZhLocale() {
		return fmt.Sprintf("%d/%d %d:%02d", t.Month(), t.Day(), t.Hour(), t.Minute())
	}
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
