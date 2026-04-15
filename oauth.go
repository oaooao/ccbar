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

const (
	oauthCacheTTL = 60 * time.Second
	oauthLockTTL  = 30 * time.Second
	apiTimeout    = 5 * time.Second
	usageEndpoint = "https://api.anthropic.com/api/oauth/usage"
)

// resolveRateLimits returns rate limit data, preferring stdin data with OAuth API fallback.
func resolveRateLimits(input *StatusInput) (fiveHour, sevenDay *ResolvedRateLimit) {
	// Prefer stdin data
	if input.RateLimits != nil {
		if rl := input.RateLimits.FiveHour; rl != nil {
			fiveHour = &ResolvedRateLimit{
				Percentage: rl.UsedPercentage,
				ResetsAt:   time.Unix(int64(rl.ResetsAt), 0),
			}
		}
		if rl := input.RateLimits.SevenDay; rl != nil {
			sevenDay = &ResolvedRateLimit{
				Percentage: rl.UsedPercentage,
				ResetsAt:   time.Unix(int64(rl.ResetsAt), 0),
			}
		}
		if fiveHour != nil || sevenDay != nil {
			return
		}
	}

	// Fallback: OAuth API
	usage := getOAuthUsage()
	if usage == nil {
		return nil, nil
	}

	if usage.FiveHour != nil {
		if t, err := time.Parse(time.RFC3339, usage.FiveHour.ResetsAt); err == nil {
			fiveHour = &ResolvedRateLimit{
				Percentage: usage.FiveHour.Utilization,
				ResetsAt:   t,
			}
		}
	}
	if usage.SevenDay != nil {
		if t, err := time.Parse(time.RFC3339, usage.SevenDay.ResetsAt); err == nil {
			sevenDay = &ResolvedRateLimit{
				Percentage: usage.SevenDay.Utilization,
				ResetsAt:   t,
			}
		}
	}
	return
}

// getOAuthUsage fetches rate limit data from the Anthropic API with caching and locking.
func getOAuthUsage() *OAuthUsageResponse {
	cacheFile := filepath.Join(os.TempDir(), "ccbar-oauth-usage.json")
	lockFile := filepath.Join(os.TempDir(), "ccbar-oauth.lock")

	// Check file cache
	cached := readOAuthCache(cacheFile)
	if cached != nil {
		if info, err := os.Stat(cacheFile); err == nil {
			if time.Since(info.ModTime()) < oauthCacheTTL {
				return cached
			}
		}
	}

	// Try to acquire lock (non-blocking)
	if !acquireLock(lockFile) {
		// Another process is fetching — return stale cache
		return cached
	}
	defer releaseLock(lockFile)

	// Get OAuth token
	token := getOAuthToken()
	if token == "" {
		return cached // stale-while-revalidate
	}

	// Call API
	usage := callUsageAPI(token)
	if usage == nil {
		return cached // stale-while-revalidate
	}

	// Write cache
	if data, err := json.Marshal(usage); err == nil {
		_ = os.WriteFile(cacheFile, data, 0644)
	}

	return usage
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

// callUsageAPI calls the Anthropic usage endpoint.
func callUsageAPI(token string) *OAuthUsageResponse {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", usageEndpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var usage OAuthUsageResponse
	if json.Unmarshal(body, &usage) != nil {
		return nil
	}

	return &usage
}

func readOAuthCache(path string) *OAuthUsageResponse {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var usage OAuthUsageResponse
	if json.Unmarshal(data, &usage) != nil {
		return nil
	}
	return &usage
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
