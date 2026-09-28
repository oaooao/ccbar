package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// modelNamesPath is the one table mapping model codenames to their real names.
// The zsh launchers in ai-cli.zsh read the same file to set Claude Code's
// ANTHROPIC_*_NAME variables, so the status line and the /model picker can
// never disagree. Overridable for tests and via CCBAR_MODEL_NAMES.
var modelNamesPath = defaultModelNamesPath()

// modelNameCache holds the parsed table for the lifetime of the process.
// nil means "not loaded yet"; an empty (non-nil) map means "loaded, nothing in it".
var modelNameCache map[string]string

func defaultModelNamesPath() string {
	if p := os.Getenv("CCBAR_MODEL_NAMES"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ai", "model-names.json")
}

func loadModelNames() map[string]string {
	if modelNameCache != nil {
		return modelNameCache
	}
	modelNameCache = map[string]string{}
	if modelNamesPath == "" {
		return modelNameCache
	}
	data, err := os.ReadFile(modelNamesPath)
	if err != nil {
		return modelNameCache
	}
	var raw map[string]string
	if json.Unmarshal(data, &raw) != nil {
		return modelNameCache
	}
	for k, v := range raw {
		// Keys starting with "_" are comments, not codenames.
		if strings.HasPrefix(k, "_") {
			continue
		}
		modelNameCache[strings.ToLower(k)] = v
	}
	return modelNameCache
}

// displayModelName turns a model codename into the real model name, keeping the
// [1m] marker as a "(1M)" suffix. Anything the table does not know passes
// through untouched — Claude Code's own models already arrive with real names
// like "Opus 4.6", and a missing table must never blank the status line.
func displayModelName(model string) string {
	if model == "" {
		return model
	}
	bare, suffix := model, ""
	if lower := strings.ToLower(model); strings.HasSuffix(lower, "[1m]") {
		bare = model[:len(model)-len("[1m]")]
		suffix = " (1M)"
	}
	if name, ok := loadModelNames()[strings.ToLower(bare)]; ok {
		return name + suffix
	}
	return model
}
