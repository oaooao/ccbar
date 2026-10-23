package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const gitCacheTTL = 5 * time.Second

// gitCache is what a render saw of a directory; Info is nil outside a repo.
type gitCache struct {
	Info *GitInfo `json:"info"`
}

// getGitInfo returns branch, staged, and modified file counts of the repo at
// cwd, or nil outside a repo. The cache is per directory, so sessions in one
// repo share a git call; when git fails or times out, the last answer stands.
func getGitInfo(cwd string) *GitInfo {
	cacheFile := cachePath("git-" + hashKey(cwd) + ".json")

	var cached *gitCache
	if data, err := os.ReadFile(cacheFile); err == nil {
		var c gitCache
		if json.Unmarshal(data, &c) == nil {
			cached = &c
		}
	}
	if cached != nil {
		if info, err := os.Stat(cacheFile); err == nil && time.Since(info.ModTime()) < gitCacheTTL {
			return cached.Info
		}
	}

	gi, err := readGitStatus(cwd)
	if err != nil {
		if cached == nil {
			return nil
		}
		t := time.Now()
		_ = os.Chtimes(cacheFile, t, t) // retry after the TTL, not every render
		return cached.Info
	}
	if data, err := json.Marshal(gitCache{Info: gi}); err == nil {
		writeCacheAtomic(cacheFile, data)
	}
	return gi
}

// readGitStatus asks git once. --no-optional-locks keeps it off index.lock,
// which a status line refreshing the index would otherwise take from under
// the git commands of the agent it reports on; -uno skips scanning for
// untracked files, which the counts don't include. A nil GitInfo with a nil
// error means cwd is not in a repo.
func readGitStatus(cwd string) (*GitInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", cwd,
		"status", "--porcelain=v2", "--branch", "--untracked-files=no")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) {
			return nil, nil // git ran and said no: not a repo
		}
		return nil, err
	}
	return parseGitStatus(string(out)), nil
}

// parseGitStatus reads `git status --porcelain=v2 --branch`: the branch,
// or the short commit when detached; files with staged changes; files with
// unstaged changes, conflicts included.
func parseGitStatus(out string) *GitInfo {
	gi := &GitInfo{}
	var head, oid string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			head = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			if len(line) < 4 {
				continue
			}
			if line[2] != '.' {
				gi.Staged++
			}
			if line[3] != '.' {
				gi.Modified++
			}
		case strings.HasPrefix(line, "u "):
			gi.Modified++
		}
	}
	gi.Branch = head
	if head == "(detached)" {
		gi.Branch = ""
		if len(oid) >= 7 {
			gi.Branch = oid[:7]
		}
	}
	return gi
}
