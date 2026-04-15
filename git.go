package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const gitCacheTTL = 5 * time.Second

// getGitInfo returns branch, staged, and modified file counts with file-based caching.
func getGitInfo(cwd, sessionID string) *GitInfo {
	cacheFile := filepath.Join(os.TempDir(), "ccbar-git-"+sessionID)

	// Check cache
	if info, err := os.Stat(cacheFile); err == nil {
		if time.Since(info.ModTime()) < gitCacheTTL {
			if data, err := os.ReadFile(cacheFile); err == nil {
				var gi GitInfo
				if json.Unmarshal(data, &gi) == nil {
					return &gi
				}
			}
		}
	}

	// Check if inside a git repo
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--git-dir")
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil
	}

	gi := &GitInfo{}

	// Branch name
	if out, err := gitExec(cwd, "branch", "--show-current"); err == nil && out != "" {
		gi.Branch = out
	} else if out, err := gitExec(cwd, "rev-parse", "--short", "HEAD"); err == nil {
		gi.Branch = out
	}

	// Staged files
	if out, err := gitExec(cwd, "diff", "--cached", "--numstat"); err == nil && out != "" {
		gi.Staged = len(strings.Split(strings.TrimSpace(out), "\n"))
	}

	// Modified files
	if out, err := gitExec(cwd, "diff", "--numstat"); err == nil && out != "" {
		gi.Modified = len(strings.Split(strings.TrimSpace(out), "\n"))
	}

	// Write cache
	if data, err := json.Marshal(gi); err == nil {
		_ = os.WriteFile(cacheFile, data, 0644)
	}

	return gi
}

func gitExec(cwd string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", cwd}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
