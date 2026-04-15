package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// getConfigStats counts CLAUDE.md files, MCP servers, and hooks with session-level caching.
func getConfigStats(cwd, sessionID string) ConfigStats {
	cacheFile := filepath.Join(os.TempDir(), "ccbar-config-"+sessionID)

	// Session-level cache (no TTL — valid for entire session)
	if data, err := os.ReadFile(cacheFile); err == nil {
		var cs ConfigStats
		if json.Unmarshal(data, &cs) == nil {
			return cs
		}
	}

	home, _ := os.UserHomeDir()
	stats := ConfigStats{}

	// Count CLAUDE.md files: walk up from cwd
	dir := cwd
	seen := map[string]bool{}
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			resolved = dir
		}
		abs, err := filepath.Abs(resolved)
		if err != nil {
			break
		}
		if seen[abs] {
			break
		}
		seen[abs] = true

		if fileExists(filepath.Join(abs, "CLAUDE.md")) {
			stats.ClaudeMdCount++
		}
		if fileExists(filepath.Join(abs, ".claude", "CLAUDE.md")) {
			stats.ClaudeMdCount++
		}

		parent := filepath.Dir(abs)
		if parent == abs { // reached root
			break
		}
		dir = parent
	}

	// User global CLAUDE.md
	globalMd := filepath.Join(home, ".claude", "CLAUDE.md")
	if !seen[home] && fileExists(globalMd) {
		stats.ClaudeMdCount++
	}

	// Count hooks from settings
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if data, err := os.ReadFile(settingsPath); err == nil {
		var settings struct {
			Hooks map[string][]struct {
				Hooks []json.RawMessage `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(data, &settings) == nil {
			for _, matchers := range settings.Hooks {
				for _, m := range matchers {
					stats.HooksCount += len(m.Hooks)
				}
			}
		}
	}

	// Count MCP servers
	claudeJsonPath := filepath.Join(home, ".claude.json")
	if data, err := os.ReadFile(claudeJsonPath); err == nil {
		var claudeJson struct {
			McpServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(data, &claudeJson) == nil {
			stats.McpCount = len(claudeJson.McpServers)
		}
	}

	// Write cache
	if data, err := json.Marshal(stats); err == nil {
		_ = os.WriteFile(cacheFile, data, 0644)
	}

	return stats
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
