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

	// ── Line 1: Identity ────────────────────────────────────────────────────

	var line1Parts []string

	// Model
	if isDefaultModel {
		line1Parts = append(line1Parts, cyan+modelName+reset)
	} else {
		line1Parts = append(line1Parts, boldYellow+strings.ToUpper(modelName)+reset)
	}

	// Project
	line1Parts = append(line1Parts, boldWhite+projectName+reset)

	// Branch with ⎇ icon
	if gitInfo != nil && gitInfo.Branch != "" {
		branchStr := muted + "⎇" + reset + " " + magenta + gitInfo.Branch + reset
		var dirty []string
		if gitInfo.Staged > 0 {
			dirty = append(dirty, green+fmt.Sprintf("+%d", gitInfo.Staged)+reset)
		}
		if gitInfo.Modified > 0 {
			dirty = append(dirty, yellow+fmt.Sprintf("~%d", gitInfo.Modified)+reset)
		}
		if len(dirty) > 0 {
			branchStr += " " + strings.Join(dirty, " ")
		}
		line1Parts = append(line1Parts, branchStr)
	}

	// Cost + Duration
	line1Parts = append(line1Parts, secondary+fmt.Sprintf("$%.2f", cost)+reset)
	line1Parts = append(line1Parts, secondary+formatDuration(durationMs)+reset)

	fmt.Println(" " + strings.Join(line1Parts, sep))

	// ── Line 2: Config Stats ────────────────────────────────────────────────

	var cfgParts []string
	if config.ClaudeMdCount > 0 {
		cfgParts = append(cfgParts, muted+fmt.Sprintf("%d memory files", config.ClaudeMdCount)+reset)
	}
	if config.McpCount > 0 {
		cfgParts = append(cfgParts, muted+fmt.Sprintf("%d mcp", config.McpCount)+reset)
	}
	if config.HooksCount > 0 {
		cfgParts = append(cfgParts, muted+fmt.Sprintf("%d hooks", config.HooksCount)+reset)
	}
	fmt.Println(" " + strings.Join(cfgParts, cfgSep))

	// ── Line 3: Context Window Bar ──────────────────────────────────────────

	fmt.Printf(" %s  %s %s%s%s\n",
		label("context"),
		contextBar(pct, ctxBarW),
		pctColor(pct), padPct(pct), reset)

	// ── Line 4: 5h Rate Limit ───────────────────────────────────────────────

	if fiveHour != nil {
		fhPct := int(math.Round(fiveHour.Percentage))
		resetStr := secondary + "⟳ " + formatResetTime(fiveHour.ResetsAt) + reset
		fmt.Printf(" %s  %s %s%s%s  %s\n",
			label("5h"), rateBar(fhPct, rateBarW),
			pctColor(fhPct), padPct(fhPct), reset, resetStr)
	} else {
		fmt.Printf(" %s  %s %s  —%s\n",
			label("5h"), rateBar(0, rateBarW), muted, reset)
	}

	// ── Line 5: 7d Rate Limit ───────────────────────────────────────────────

	if sevenDay != nil {
		sdPct := int(math.Round(sevenDay.Percentage))
		resetStr := secondary + "⟳ " + formatResetDateTime(sevenDay.ResetsAt) + reset
		fmt.Printf(" %s  %s %s%s%s  %s\n",
			label("weekly"), rateBar(sdPct, rateBarW),
			pctColor(sdPct), padPct(sdPct), reset, resetStr)
	} else {
		fmt.Printf(" %s  %s %s  —%s\n",
			label("weekly"), rateBar(0, rateBarW), muted, reset)
	}
}
