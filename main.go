package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// hiddenSections tracks which sections to hide via --hide flag.
var hiddenSections = map[string]bool{}

func parseFlags() {
	args := os.Args[1:]

	// Check for subcommands first
	if len(args) > 0 && args[0] == "setup" {
		runSetup()
		os.Exit(0)
	}

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--version":
			fmt.Println(formatVersion())
			os.Exit(0)
		case args[i] == "--help" || args[i] == "-h":
			printHelp()
			os.Exit(0)
		case args[i] == "--theme" && i+1 < len(args):
			i++
			applyTheme(args[i])
		case strings.HasPrefix(args[i], "--theme="):
			applyTheme(strings.TrimPrefix(args[i], "--theme="))
		case args[i] == "--locale" && i+1 < len(args):
			i++
			applyLocale(args[i])
		case strings.HasPrefix(args[i], "--locale="):
			applyLocale(strings.TrimPrefix(args[i], "--locale="))
		case args[i] == "--hide" && i+1 < len(args):
			i++
			applyHide(args[i])
		case strings.HasPrefix(args[i], "--hide="):
			applyHide(strings.TrimPrefix(args[i], "--hide="))
		}
	}
}

func applyTheme(v string) {
	switch v {
	case "light":
		th = lightTheme
	case "dark":
		th = darkTheme
	}
}

func applyLocale(v string) {
	switch v {
	case "zh":
		localeOverride = "zh"
	case "en":
		localeOverride = "en"
	}
}

func applyHide(v string) {
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			hiddenSections[s] = true
		}
	}
}

func isVisible(section string) bool {
	return !hiddenSections[section]
}

func printHelp() {
	fmt.Println(`ccbar — A beautifully designed status line for Claude Code

Usage:
  ccbar [flags]          Read session JSON from stdin and print status line
  ccbar setup            Configure Claude Code to use ccbar (interactive)

Flags:
  --theme <dark|light>   Set color theme (default: dark)
  --locale <en|zh>       Set date format (default: auto-detect from system)
  --hide <sections>      Hide sections, comma-separated: config,context,5h,weekly
  --version              Print version
  --help                 Print this help

Examples:
  ccbar --theme light
  ccbar --hide 5h,weekly
  ccbar --theme light --locale zh --hide config`)
}

func main() {
	parseFlags()

	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		return
	}

	var data StatusInput
	if json.Unmarshal(input, &data) != nil {
		return
	}

	cwd := data.Workspace.CurrentDir
	if cwd == "" {
		cwd = data.Cwd
	}
	if cwd == "" {
		return
	}

	projectName := filepath.Base(cwd)
	modelName := data.Model.DisplayName
	if modelName == "" {
		modelName = "Unknown"
	}
	isDefaultModel := strings.Contains(strings.ToLower(modelName), "opus")

	pct := 0
	if data.ContextWindow.UsedPercentage != nil {
		pct = int(math.Floor(*data.ContextWindow.UsedPercentage))
	}

	cost := 0.0
	durationMs := 0.0
	if data.Cost != nil {
		cost = data.Cost.TotalCostUSD
		durationMs = data.Cost.TotalDurationMs
	}

	sessionID := data.SessionID
	if sessionID == "" {
		sessionID = "unknown"
	}

	// Gather data
	gitInfo := getGitInfo(cwd, sessionID)
	config := getConfigStats(cwd, sessionID)
	fiveHour, sevenDay := resolveRateLimits(&data)

	sep := sepStr()

	// ── Line 1: Identity (always visible) ───────────────────────────────────

	var line1Parts []string

	if isDefaultModel {
		line1Parts = append(line1Parts, th.Cyan+modelName+th.Reset)
	} else {
		line1Parts = append(line1Parts, th.BoldYellow+strings.ToUpper(modelName)+th.Reset)
	}

	line1Parts = append(line1Parts, th.BoldWhite+projectName+th.Reset)

	if gitInfo != nil && gitInfo.Branch != "" {
		branchStr := th.Muted + "⎇" + th.Reset + " " + th.Magenta + gitInfo.Branch + th.Reset
		var dirty []string
		if gitInfo.Staged > 0 {
			dirty = append(dirty, th.Green+fmt.Sprintf("+%d", gitInfo.Staged)+th.Reset)
		}
		if gitInfo.Modified > 0 {
			dirty = append(dirty, th.Yellow+fmt.Sprintf("~%d", gitInfo.Modified)+th.Reset)
		}
		if len(dirty) > 0 {
			branchStr += " " + strings.Join(dirty, " ")
		}
		line1Parts = append(line1Parts, branchStr)
	}

	line1Parts = append(line1Parts, th.Secondary+fmt.Sprintf("$%.2f", cost)+th.Reset)
	line1Parts = append(line1Parts, th.Secondary+formatDuration(durationMs)+th.Reset)

	fmt.Println(" " + strings.Join(line1Parts, sep))

	// ── Line 2: Config Stats ────────────────────────────────────────────────

	if isVisible("config") {
		cfgSep := cfgSepStr()
		var cfgParts []string
		if config.ClaudeMdCount > 0 {
			cfgParts = append(cfgParts, th.Muted+fmt.Sprintf("%d memory files", config.ClaudeMdCount)+th.Reset)
		}
		if config.McpCount > 0 {
			cfgParts = append(cfgParts, th.Muted+fmt.Sprintf("%d mcp", config.McpCount)+th.Reset)
		}
		if config.HooksCount > 0 {
			cfgParts = append(cfgParts, th.Muted+fmt.Sprintf("%d hooks", config.HooksCount)+th.Reset)
		}
		if len(cfgParts) > 0 {
			fmt.Println(" " + strings.Join(cfgParts, cfgSep))
		}
	}

	// ── Line 3: Context Window Bar ──────────────────────────────────────────

	if isVisible("context") {
		fmt.Printf(" %s  %s %s%s%s\n",
			label("context"),
			contextBar(pct, ctxBarW),
			pctColor(pct), padPct(pct), th.Reset)
	}

	// ── Line 4: 5h Rate Limit ───────────────────────────────────────────────

	if isVisible("5h") {
		if fiveHour != nil {
			fhPct := int(math.Round(fiveHour.Percentage))
			resetStr := th.Secondary + "⟳ " + formatResetTime(fiveHour.ResetsAt) + th.Reset
			fmt.Printf(" %s  %s %s%s%s  %s\n",
				label("5h"), rateBar(fhPct, rateBarW),
				pctColor(fhPct), padPct(fhPct), th.Reset, resetStr)
		} else {
			fmt.Printf(" %s  %s %s  —%s\n",
				label("5h"), rateBar(0, rateBarW), th.Muted, th.Reset)
		}
	}

	// ── Line 5: 7d Rate Limit ───────────────────────────────────────────────

	if isVisible("weekly") {
		if sevenDay != nil {
			sdPct := int(math.Round(sevenDay.Percentage))
			resetStr := th.Secondary + "⟳ " + formatResetDateTime(sevenDay.ResetsAt) + th.Reset
			fmt.Printf(" %s  %s %s%s%s  %s\n",
				label("weekly"), rateBar(sdPct, rateBarW),
				pctColor(sdPct), padPct(sdPct), th.Reset, resetStr)
		} else {
			fmt.Printf(" %s  %s %s  —%s\n",
				label("weekly"), rateBar(0, rateBarW), th.Muted, th.Reset)
		}
	}
}
