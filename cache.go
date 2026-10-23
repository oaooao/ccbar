package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// housekeepEvery is how often a render may list the cache directory.
	housekeepEvery = time.Hour
	// staleCacheAge is when a per-session or per-repo cache counts as left
	// behind by a session long gone.
	staleCacheAge = 7 * 24 * time.Hour
)

// cacheDir is ccbar's own directory under the system temp dir. Every cache,
// lock and temp file lives here, so a render reads only what ccbar wrote and
// never lists the shared temp dir, which other programs can fill: in
// 2026-10 a test suite left 479k entries there, listing it took minutes, and
// the renders of 45 sessions piled up in I/O wait.
func cacheDir() string {
	dir := filepath.Join(os.TempDir(), "ccbar")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func cachePath(name string) string { return filepath.Join(cacheDir(), name) }

// hashKey turns a path into a short file-name-safe key.
func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// housekeep removes temp files of crashed writers and caches of sessions
// long gone, at most once per housekeepEvery: only then does a render pay
// for listing the directory.
func housekeep(t time.Time) {
	dir := cacheDir()
	stamp := filepath.Join(dir, "housekeep.stamp")
	if info, err := os.Stat(stamp); err == nil && t.Sub(info.ModTime()) < housekeepEvery {
		return
	}
	if f, err := os.OpenFile(stamp, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Close()
	}
	_ = os.Chtimes(stamp, t, t) // renders from now on skip

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		var maxAge time.Duration
		switch {
		case strings.HasPrefix(name, tmpCachePrefix):
			maxAge = oauthLockTTL // no write takes this long
		case strings.HasPrefix(name, "git-"), strings.HasPrefix(name, "config-"):
			maxAge = staleCacheAge
		default:
			continue
		}
		if info, err := e.Info(); err == nil && t.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}
