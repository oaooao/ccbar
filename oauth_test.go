package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func oauthBucket(pct float64, t time.Time) *OAuthRateLimit {
	return &OAuthRateLimit{Utilization: pct, ResetsAt: t.UTC().Format(time.RFC3339)}
}

func stdinBucket(pct float64, t time.Time) *RateLimit {
	return &RateLimit{UsedPercentage: pct, ResetsAt: float64(t.Unix())}
}

func makeStdin(fiveHour *RateLimit, weekly ...*RateLimit) *StatusInput {
	rl := &RateLimits{FiveHour: fiveHour}
	if len(weekly) > 0 {
		rl.SevenDay = weekly[0]
	}
	if len(weekly) > 1 {
		rl.SevenDayOpus = weekly[1]
	}
	if len(weekly) > 2 {
		rl.SevenDaySonnet = weekly[2]
	}
	return &StatusInput{RateLimits: rl}
}

func result(state oauthState, usage *OAuthUsageResponse) oauthResult {
	return oauthResult{usage: usage, state: state, reason: reasonCacheHit}
}

// ---------------------------------------------------------------------------
// A 组：策略层（resolveFiveHour / resolveWeekly）
// ---------------------------------------------------------------------------

func TestResolveFiveHour_FreshOAuthWins(t *testing.T) {
	future := time.Now().Add(time.Hour)
	r := result(oauthFresh, &OAuthUsageResponse{FiveHour: oauthBucket(80, future)})
	in := makeStdin(stdinBucket(10, future))
	got, src := resolveFiveHour(in, r)
	if got == nil || got.Percentage != 80 {
		t.Fatalf("expected OAuth (80), got %+v", got)
	}
	if src != srcOAuth {
		t.Fatalf("src = %s, want oauth", src)
	}
}

func TestResolveFiveHour_FreshOAuthMissingBucket_UsesStdin(t *testing.T) {
	future := time.Now().Add(time.Hour)
	r := result(oauthFresh, &OAuthUsageResponse{}) // FiveHour nil
	in := makeStdin(stdinBucket(42, future))
	got, src := resolveFiveHour(in, r)
	if got == nil || got.Percentage != 42 {
		t.Fatalf("expected stdin (42), got %+v", got)
	}
	if src != srcStdin {
		t.Fatalf("src = %s, want stdin", src)
	}
}

func TestResolveFiveHour_FreshOAuthMissing_NoStdin_Nil(t *testing.T) {
	r := result(oauthFresh, &OAuthUsageResponse{})
	got, src := resolveFiveHour(nil, r)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if src != srcNone {
		t.Fatalf("src = %s, want none", src)
	}
}

func TestResolveFiveHour_StaleOAuth_StdinWins(t *testing.T) {
	future := time.Now().Add(time.Hour)
	r := result(oauthStale, &OAuthUsageResponse{FiveHour: oauthBucket(90, future)})
	in := makeStdin(stdinBucket(15, future))
	got, src := resolveFiveHour(in, r)
	// Product policy: stale OAuth defers to local stdin snapshot.
	if got == nil || got.Percentage != 15 {
		t.Fatalf("expected stdin (15), got %+v", got)
	}
	if src != srcStdin {
		t.Fatalf("src = %s, want stdin", src)
	}
}

func TestResolveFiveHour_StaleOAuth_NoStdin_UsesOAuth(t *testing.T) {
	future := time.Now().Add(time.Hour)
	r := result(oauthStale, &OAuthUsageResponse{FiveHour: oauthBucket(90, future)})
	got, src := resolveFiveHour(nil, r)
	if got == nil || got.Percentage != 90 {
		t.Fatalf("expected stale OAuth (90), got %+v", got)
	}
	if src != srcOAuth {
		t.Fatalf("src = %s, want oauth", src)
	}
}

func TestResolveFiveHour_Unavailable_StdinWins(t *testing.T) {
	future := time.Now().Add(time.Hour)
	r := oauthResult{state: oauthUnavailable, reason: reasonTokenMissing} // usage nil by invariant
	in := makeStdin(stdinBucket(33, future))
	got, src := resolveFiveHour(in, r)
	if got == nil || got.Percentage != 33 {
		t.Fatalf("expected stdin (33), got %+v", got)
	}
	if src != srcStdin {
		t.Fatalf("src = %s, want stdin", src)
	}
}

func TestResolveFiveHour_Unavailable_NoStdin_Nil(t *testing.T) {
	r := oauthResult{state: oauthUnavailable, reason: reasonTokenMissing}
	got, src := resolveFiveHour(nil, r)
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if src != srcNone {
		t.Fatalf("src = %s, want none", src)
	}
}

func TestResolveWeekly_FreshOAuth_SameSourceMax_NotCrossSource(t *testing.T) {
	future := time.Now().Add(72 * time.Hour)
	// OAuth low, stdin high. Cross-source max would pick stdin (90). Same-source picks OAuth.
	r := result(oauthFresh, &OAuthUsageResponse{
		SevenDay:       oauthBucket(20, future),
		SevenDaySonnet: oauthBucket(30, future),
	})
	in := makeStdin(nil, stdinBucket(90, future))
	got, src := resolveWeekly(in, r)
	if got == nil || got.Percentage != 30 {
		t.Fatalf("expected OAuth source max (30), got %+v", got)
	}
	if src != srcOAuth {
		t.Fatalf("src = %s, want oauth", src)
	}
}

func TestResolveFiveHour_OAuthResetsAtParseFail_FallsBackToStdin(t *testing.T) {
	future := time.Now().Add(time.Hour)
	bad := &OAuthRateLimit{Utilization: 99, ResetsAt: "not-a-date"}
	r := result(oauthFresh, &OAuthUsageResponse{FiveHour: bad})
	in := makeStdin(stdinBucket(11, future))
	got, src := resolveFiveHour(in, r)
	if got == nil || got.Percentage != 11 {
		t.Fatalf("expected stdin fallback (11) on parse-fail, got %+v", got)
	}
	if src != srcStdin {
		t.Fatalf("src = %s, want stdin", src)
	}
}

func TestResolveFiveHourAndWeekly_IndependentSources(t *testing.T) {
	// OAuth has only 5h, stdin has only weekly. Sources should diverge cleanly.
	future := time.Now().Add(time.Hour)
	r := result(oauthFresh, &OAuthUsageResponse{FiveHour: oauthBucket(55, future)})
	in := makeStdin(nil, stdinBucket(77, future))
	fh, fhSrc := resolveFiveHour(in, r)
	wk, wkSrc := resolveWeekly(in, r)
	if fh == nil || fh.Percentage != 55 || fhSrc != srcOAuth {
		t.Fatalf("five-hour: got %+v src=%s, want 55 from oauth", fh, fhSrc)
	}
	if wk == nil || wk.Percentage != 77 || wkSrc != srcStdin {
		t.Fatalf("weekly: got %+v src=%s, want 77 from stdin", wk, wkSrc)
	}
}

// ---------------------------------------------------------------------------
// B 组：契约层（getOAuthUsage 状态机）
// ---------------------------------------------------------------------------

// isolateTempDir gives each test a fresh TMPDIR so cache/lock files don't collide.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func clearOAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	// Block Keychain & credentials file fallback by pointing HOME at an empty dir.
	t.Setenv("HOME", t.TempDir())
}

func TestContract_LockHeld_NoCache_Unavailable(t *testing.T) {
	dir := isolateTempDir(t)
	clearOAuthEnv(t)
	// Hold the lock by creating it fresh.
	lockPath := filepath.Join(dir, "ccbar-oauth.lock")
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	r := getOAuthUsage()
	if r.state != oauthUnavailable || r.usage != nil || r.reason != reasonLockHeld {
		t.Fatalf("got %+v, want {nil, unavailable, lock_held}", r)
	}
}

func TestContract_TokenMissing_WithCache_Stale(t *testing.T) {
	dir := isolateTempDir(t)
	clearOAuthEnv(t)
	// Plant a valid but stale cache (mtime old).
	cachePath := filepath.Join(dir, "ccbar-oauth-usage.json")
	cache := &OAuthUsageResponse{FiveHour: oauthBucket(12, time.Now().Add(time.Hour))}
	data, _ := json.Marshal(cache)
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	r := getOAuthUsage()
	if r.state != oauthStale || r.usage == nil || r.reason != reasonTokenMissing {
		t.Fatalf("got %+v, want {usage!=nil, stale, token_missing}", r)
	}
}

func TestContract_APIFailure_NoCache_Unavailable(t *testing.T) {
	isolateTempDir(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	old := usageEndpoint
	usageEndpoint = srv.URL
	defer func() { usageEndpoint = old }()

	r := getOAuthUsage()
	if r.state != oauthUnavailable || r.usage != nil || r.reason != reasonAPIFailed {
		t.Fatalf("got %+v, want {nil, unavailable, api_failed}", r)
	}
}

func TestContract_CorruptCache_TreatedAsMissing(t *testing.T) {
	dir := isolateTempDir(t)
	clearOAuthEnv(t)
	// Write half-truncated JSON — parse will fail.
	cachePath := filepath.Join(dir, "ccbar-oauth-usage.json")
	if err := os.WriteFile(cachePath, []byte(`{"five_hour": {"utiliza`), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(cachePath, old, old)

	r := getOAuthUsage()
	// Corrupt cache never returns as stale. With no token, we must be unavailable.
	if r.state != oauthUnavailable || r.usage != nil {
		t.Fatalf("corrupt cache leaked as stale: got %+v", r)
	}
	if !r.cacheCorrupt {
		t.Fatal("cacheCorrupt flag not set")
	}
}

func TestContract_Invariant_NilIffUnavailable(t *testing.T) {
	// Sweep all branches and assert: usage == nil  iff  state == oauthUnavailable.
	branches := []func() oauthResult{
		// fresh via cache hit
		func() oauthResult {
			dir := isolateTempDir(t)
			cache := &OAuthUsageResponse{FiveHour: oauthBucket(1, time.Now().Add(time.Hour))}
			data, _ := json.Marshal(cache)
			_ = os.WriteFile(filepath.Join(dir, "ccbar-oauth-usage.json"), data, 0644)
			return getOAuthUsage()
		},
		// lock_held no cache → unavailable
		func() oauthResult {
			dir := isolateTempDir(t)
			clearOAuthEnv(t)
			_ = os.WriteFile(filepath.Join(dir, "ccbar-oauth.lock"), nil, 0644)
			return getOAuthUsage()
		},
		// token_missing no cache → unavailable
		func() oauthResult {
			isolateTempDir(t)
			clearOAuthEnv(t)
			return getOAuthUsage()
		},
	}
	for i, b := range branches {
		r := b()
		unavailable := r.state == oauthUnavailable
		nilUsage := r.usage == nil
		if unavailable != nilUsage {
			t.Fatalf("branch %d violates invariant: state=%v usage=%v", i, r.state, r.usage)
		}
	}
}

// ---------------------------------------------------------------------------
// C 组：并发原子写
// ---------------------------------------------------------------------------

func TestWriteCacheAtomic_ConcurrentReadersSeeValidJSON(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "ccbar-oauth-usage.json")

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writers: repeatedly write distinct payloads.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				payload := fmt.Sprintf(`{"writer":%d,"seq":%d}`, id, j)
				writeCacheAtomic(cacheFile, []byte(payload))
			}
		}(i)
	}

	// Readers: parse as JSON and fail if ever truncated.
	var readErr error
	var readMu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := os.ReadFile(cacheFile)
				if err != nil {
					continue // file may transiently not exist before first writer wins
				}
				var v map[string]any
				if jerr := json.Unmarshal(data, &v); jerr != nil {
					readMu.Lock()
					readErr = fmt.Errorf("reader saw invalid JSON: %v, payload=%q", jerr, string(data))
					readMu.Unlock()
					return
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	if readErr != nil {
		t.Fatal(readErr)
	}
}

func TestCleanupOrphanTmpFiles(t *testing.T) {
	dir := t.TempDir()
	// Create one fresh tmp (should survive) and one old tmp (should be removed).
	fresh := filepath.Join(dir, "ccbar-oauth-tmp-fresh.json")
	old := filepath.Join(dir, "ccbar-oauth-tmp-old.json")
	_ = os.WriteFile(fresh, []byte("{}"), 0644)
	_ = os.WriteFile(old, []byte("{}"), 0644)
	oldTime := time.Now().Add(-2 * oauthLockTTL)
	_ = os.Chtimes(old, oldTime, oldTime)

	// Also plant a file that MUST NOT be touched (wrong prefix).
	bystander := filepath.Join(dir, "ccbar-oauth-usage.json")
	_ = os.WriteFile(bystander, []byte("{}"), 0644)
	_ = os.Chtimes(bystander, oldTime, oldTime)

	cleanupOrphanTmpFiles(dir)

	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh tmp incorrectly removed: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old tmp not removed: err=%v", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("real cache (bystander) was deleted — glob matched too broadly: %v", err)
	}
}
