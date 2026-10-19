package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const tmpCachePrefix = "ccbar-limits-tmp-"

const (
	oauthLockTTL = 30 * time.Second
	apiTimeout   = 5 * time.Second
)

// usageEndpoint is var (not const) so tests can point it at an httptest server.
var usageEndpoint = "https://api.anthropic.com/api/oauth/usage"

// apiOutcome is what one call of the usage endpoint came to.
type apiOutcome string

const (
	apiSkipped     apiOutcome = "skipped"      // not due, or backing off
	apiOK          apiOutcome = "ok"           // 200 with a usage body
	apiRateLimited apiOutcome = "rate_limited" // 429: the account's budget is spent
	apiFailed      apiOutcome = "failed"       // network, other status, bad body
	apiLockHeld    apiOutcome = "lock_held"    // another session is calling
	apiNoToken     apiOutcome = "no_token"
)

// callUsageAPI calls the Anthropic usage endpoint.
func callUsageAPI(token string) (*OAuthUsageResponse, apiOutcome) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", usageEndpoint, nil)
	if err != nil {
		return nil, apiFailed
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, apiFailed
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, apiRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, apiFailed
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, apiFailed
	}

	var usage OAuthUsageResponse
	if json.Unmarshal(body, &usage) != nil {
		return nil, apiFailed
	}
	return &usage, apiOK
}

// writeCacheAtomic writes via temp file + rename so readers never see a half-written JSON.
func writeCacheAtomic(cacheFile string, data []byte) {
	dir := filepath.Dir(cacheFile)
	f, err := os.CreateTemp(dir, tmpCachePrefix+"*.json")
	if err != nil {
		return
	}
	tmpPath := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, cacheFile); err != nil {
		_ = os.Remove(tmpPath)
	}
}

// cleanupOrphanTmpFiles removes leftover temp cache files from crashed writers.
// The tmp prefix is disjoint from the real cache filename, so the real cache is never touched.
func cleanupOrphanTmpFiles(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, tmpCachePrefix+"*.json"))
	if err != nil {
		return
	}
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > oauthLockTTL {
			_ = os.Remove(p)
		}
	}
}

// getOAuthToken retrieves the OAuth token via three-level fallback.
func getOAuthToken() string {
	// 1. Environment variable
	if token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		return token
	}

	// 2. macOS Keychain
	if out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output(); err == nil {
		token := parseCredentialToken(out)
		if token != "" {
			return token
		}
	}

	// 3. Credentials file
	home, _ := os.UserHomeDir()
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	if data, err := os.ReadFile(credPath); err == nil {
		token := parseCredentialToken(data)
		if token != "" {
			return token
		}
	}

	return ""
}

// parseCredentialToken extracts accessToken from Claude credential JSON.
// The JSON can be either {claudeAiOauth: {accessToken: "..."}} or {accessToken: "..."}.
func parseCredentialToken(data []byte) string {
	// Try nested format first
	var nested struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &nested) == nil && nested.ClaudeAiOauth.AccessToken != "" {
		return nested.ClaudeAiOauth.AccessToken
	}

	// Try flat format
	var flat struct {
		AccessToken string `json:"accessToken"`
	}
	if json.Unmarshal(data, &flat) == nil && flat.AccessToken != "" {
		return flat.AccessToken
	}

	return ""
}

func acquireLock(path string) bool {
	// Clean stale lock
	if info, err := os.Stat(path); err == nil {
		if time.Since(info.ModTime()) > oauthLockTTL {
			_ = os.Remove(path)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return false // lock held by another process
	}
	f.Close()
	return true
}

func releaseLock(path string) {
	_ = os.Remove(path)
}

// version is set by ldflags at build time.
var version = "dev"

func formatVersion() string {
	return fmt.Sprintf("ccbar %s", version)
}
