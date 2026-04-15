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

func main() {
	// Parse --theme flag
	for i, arg := range os.Args[1:] {
		if arg == "--theme" && i+1 < len(os.Args)-1 {
			switch os.Args[i+2] {
			case "light":
				th = lightTheme
			case "dark":
				th = darkTheme
			}
		} else if arg == "--theme=light" {
			th = lightTheme
		} else if arg == "--theme=dark" {
			th = darkTheme
		} else if arg == "--version" {
			fmt.Println(formatVersion())
			return
		}
	}

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

	// ── Line 1: Identity ────────────────────────────────────────────────────

	var line1Parts []string

	// Model
	if isDefaultModel {
		line1Parts = append(line1Parts, th.Cyan+modelName+th.Reset)
	} else {
		line1Parts = append(line1Parts, th.BoldYellow+strings.ToUpper(modelName)+th.Reset)
	}

	// Project
	line1Parts = append(line1Parts, th.BoldWhite+projectName+th.Reset)

	// Branch with ⎇ icon
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

	// Cost + Duration
	line1Parts = append(line1Parts, th.Secondary+fmt.Sprintf("$%.2f", cost)+th.Reset)
	line1Parts = append(line1Parts, th.Secondary+formatDuration(durationMs)+th.Reset)

	fmt.Println(" " + strings.Join(line1Parts, sep))

	// ── Line 2: Config Stats ────────────────────────────────────────────────

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
	fmt.Println(" " + strings.Join(cfgParts, cfgSep))

	// ── Line 3: Context Window Bar ──────────────────────────────────────────

	fmt.Printf(" %s  %s %s%s%s\n",
		label("context"),
		contextBar(pct, ctxBarW),
		pctColor(pct), padPct(pct), th.Reset)

	// ── Line 4: 5h Rate Limit ───────────────────────────────────────────────

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

	// ── Line 5: 7d Rate Limit ───────────────────────────────────────────────

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
