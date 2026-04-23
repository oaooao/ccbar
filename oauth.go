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

const tmpCachePrefix = "ccbar-oauth-tmp-"

const (
	oauthCacheTTL = 60 * time.Second
	oauthLockTTL  = 30 * time.Second
	apiTimeout    = 5 * time.Second
	usageEndpoint = "https://api.anthropic.com/api/oauth/usage"
)

// oauthState classifies the trustworthiness of OAuth usage data for per-bucket selection.
// See docs/rate-limit-refresh-spec.md for the full decision table.
type oauthState int

const (
	oauthFresh       oauthState = iota // cache < TTL, or live API just succeeded
	oauthStale                         // usable data exists but couldn't refresh
	oauthUnavailable                   // no usable data at all
)

func (s oauthState) String() string {
	switch s {
	case oauthFresh:
		return "fresh"
	case oauthStale:
		return "stale"
	default:
		return "unavailable"
	}
}

// oauthReason is diagnostic only — resolvers must never branch on it.
type oauthReason string

const (
	reasonCacheHit     oauthReason = "cache_hit"
	reasonAPIOk        oauthReason = "api_ok"
	reasonLockHeld     oauthReason = "lock_held"
	reasonTokenMissing oauthReason = "token_missing"
	reasonAPIFailed    oauthReason = "api_failed"
)

// oauthResult carries the outcome of getOAuthUsage.
//
// Bi-directional invariant (callers may rely on this):
//
//	state == oauthUnavailable  iff  usage == nil
//	state == oauthFresh || oauthStale  ⇒  usage != nil
type oauthResult struct {
	usage        *OAuthUsageResponse
	state        oauthState
	reason       oauthReason
	cacheCorrupt bool // debug-only marker; does not change reason
}

// resolveRateLimits picks rate limit data per-bucket, OAuth-primary with stdin fallback.
//
// OAuth (with its 60s cross-process file cache) is the cross-session source of truth.
// stdin is a per-session local snapshot — used when OAuth is stale or unavailable.
// Weekly bucket aggregation is same-source only; cross-source max would lock an
// already-reset binding constraint onto the screen.
func resolveRateLimits(input *StatusInput) (fiveHour, sevenDay *ResolvedRateLimit) {
	r := getOAuthUsage()
	fiveHour, fiveSrc := resolveFiveHour(input, r)
	sevenDay, weeklySrc := resolveWeekly(input, r)
	if debugEnabled() {
		logDecision(r, fiveSrc, weeklySrc)
	}
	return
}

// debugEnabled returns true when CCBAR_DEBUG is set to any non-empty value.
func debugEnabled() bool {
	return os.Getenv("CCBAR_DEBUG") != ""
}

// logDecision writes a single JSON line to stderr describing how rate limits
// were resolved. Gated by debugEnabled so the status line never leaks noise.
func logDecision(r oauthResult, fiveSrc, weeklySrc bucketSource) {
	entry := map[string]any{
		"ts":                     time.Now().UTC().Format(time.RFC3339Nano),
		"oauth_state":            r.state.String(),
		"oauth_reason":           string(r.reason),
		"cache_corrupt_detected": r.cacheCorrupt,
		"five_hour_src":          string(fiveSrc),
		"weekly_src":             string(weeklySrc),
	}
	if data, err := json.Marshal(entry); err == nil {
		fmt.Fprintln(os.Stderr, string(data))
	}
}

// bucketSource names which source produced the picked bucket.
type bucketSource string

const (
	srcOAuth bucketSource = "oauth"
	srcStdin bucketSource = "stdin"
	srcNone  bucketSource = "none"
)

// resolveFiveHour selects the 5-hour bucket from OAuth or stdin per the decision table.
func resolveFiveHour(input *StatusInput, r oauthResult) (*ResolvedRateLimit, bucketSource) {
	var o *ResolvedRateLimit
	if r.usage != nil {
		o = parseOAuthBucket(r.usage.FiveHour)
	}
	var s *ResolvedRateLimit
	if input != nil && input.RateLimits != nil {
		s = parseStdinBucket(input.RateLimits.FiveHour)
	}
	return pickBucket(r.state, o, s)
}

// resolveWeekly selects the weekly bucket. Aggregation is same-source only.
func resolveWeekly(input *StatusInput, r oauthResult) (*ResolvedRateLimit, bucketSource) {
	var o *ResolvedRateLimit
	if r.usage != nil {
		o = pickWeeklyFromOAuth(r.usage)
	}
	var s *ResolvedRateLimit
	if input != nil && input.RateLimits != nil {
		s = pickWeeklyFromStdin(input.RateLimits)
	}
	return pickBucket(r.state, o, s)
}

// pickBucket implements the per-bucket decision table and reports the chosen source.
//
// "o / s available" means the source-specific value was successfully parsed into
// a *ResolvedRateLimit (not merely that the raw pointer was non-nil).
//
//	oauthFresh       + o → o
//	oauthFresh       + !o + s → s
//	oauthStale       + s → s  (product policy: local responsiveness over cross-session consistency)
//	oauthStale       + !s + o → o
//	oauthUnavailable + s → s  (invariant guarantees o is nil here)
//	else → nil
func pickBucket(state oauthState, o, s *ResolvedRateLimit) (*ResolvedRateLimit, bucketSource) {
	switch state {
	case oauthFresh:
		if o != nil {
			return o, srcOAuth
		}
		if s != nil {
			return s, srcStdin
		}
		return nil, srcNone
	case oauthStale:
		if s != nil {
			return s, srcStdin
		}
		if o != nil {
			return o, srcOAuth
		}
		return nil, srcNone
	default: // oauthUnavailable: invariant guarantees o is nil
		if s != nil {
			return s, srcStdin
		}
		return nil, srcNone
	}
}

// parseOAuthBucket returns a parsed ResolvedRateLimit, or nil if the raw bucket
// is nil or has an unparseable RFC3339 resets_at.
func parseOAuthBucket(b *OAuthRateLimit) *ResolvedRateLimit {
	if b == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339, b.ResetsAt)
	if err != nil {
		return nil
	}
	return &ResolvedRateLimit{Percentage: b.Utilization, ResetsAt: t}
}

// parseStdinBucket returns a parsed ResolvedRateLimit, or nil if the raw bucket is nil.
func parseStdinBucket(b *RateLimit) *ResolvedRateLimit {
	if b == nil {
		return nil
	}
	return &ResolvedRateLimit{
		Percentage: b.UsedPercentage,
		ResetsAt:   time.Unix(int64(b.ResetsAt), 0),
	}
}

// pickWeeklyFromStdin aggregates the weekly bucket within the stdin source (same-source max).
func pickWeeklyFromStdin(rl *RateLimits) *ResolvedRateLimit {
	candidates := []*RateLimit{rl.SevenDay, rl.SevenDayOpus, rl.SevenDaySonnet}
	var best *RateLimit
	for _, c := range candidates {
		if c == nil {
			continue
		}
		if best == nil || c.UsedPercentage > best.UsedPercentage {
			best = c
		}
	}
	return parseStdinBucket(best)
}

// pickWeeklyFromOAuth aggregates the weekly bucket within the OAuth source (same-source max).
func pickWeeklyFromOAuth(u *OAuthUsageResponse) *ResolvedRateLimit {
	candidates := []*OAuthRateLimit{u.SevenDay, u.SevenDayOpus, u.SevenDaySonnet}
	var best *OAuthRateLimit
	for _, c := range candidates {
		if c == nil {
			continue
		}
		if best == nil || c.Utilization > best.Utilization {
			best = c
		}
	}
	return parseOAuthBucket(best)
}

// getOAuthUsage returns the oauthResult after checking cache, lock, token, and API.
// See the decision table in docs/rate-limit-refresh-spec.md for full semantics.
func getOAuthUsage() oauthResult {
	cacheFile := filepath.Join(os.TempDir(), "ccbar-oauth-usage.json")
	lockFile := filepath.Join(os.TempDir(), "ccbar-oauth.lock")

	cleanupOrphanTmpFiles(filepath.Dir(cacheFile))

	cached, age, corrupt := loadOAuthCache(cacheFile)
	// Corrupt cache is treated as no cache — it never participates as stale data.
	if corrupt {
		cached = nil
	}

	// Fresh cache hit
	if cached != nil && age < oauthCacheTTL {
		return oauthResult{usage: cached, state: oauthFresh, reason: reasonCacheHit, cacheCorrupt: corrupt}
	}

	// Need refresh. Non-blocking lock.
	if !acquireLock(lockFile) {
		return staleOrUnavailable(cached, reasonLockHeld, corrupt)
	}
	defer releaseLock(lockFile)

	token := getOAuthToken()
	if token == "" {
		return staleOrUnavailable(cached, reasonTokenMissing, corrupt)
	}

	usage := callUsageAPI(token)
	if usage == nil {
		return staleOrUnavailable(cached, reasonAPIFailed, corrupt)
	}

	if data, err := json.Marshal(usage); err == nil {
		writeCacheAtomic(cacheFile, data)
	}
	return oauthResult{usage: usage, state: oauthFresh, reason: reasonAPIOk, cacheCorrupt: corrupt}
}

// staleOrUnavailable builds an oauthResult preserving the bi-directional invariant.
func staleOrUnavailable(cached *OAuthUsageResponse, reason oauthReason, corrupt bool) oauthResult {
	if cached != nil {
		return oauthResult{usage: cached, state: oauthStale, reason: reason, cacheCorrupt: corrupt}
	}
	return oauthResult{usage: nil, state: oauthUnavailable, reason: reason, cacheCorrupt: corrupt}
}

// loadOAuthCache reads the cache file and reports (data, age, corrupt).
//
//	corrupt = true  → file exists but JSON decode failed; caller treats as no cache
//	corrupt = false + data == nil → file missing
//	corrupt = false + data != nil → parsed successfully
func loadOAuthCache(path string) (*OAuthUsageResponse, time.Duration, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false
	}
	age := time.Since(info.ModTime())
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, age, false
	}
	var u OAuthUsageResponse
	if json.Unmarshal(data, &u) != nil {
		return nil, age, true
	}
	return &u, age, false
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
