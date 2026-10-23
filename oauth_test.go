package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var base = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

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

func obs(pct float64, resets time.Time) *observation {
	return &observation{Percentage: pct, ResetsAt: resets}
}

// fakeAPI is the usage endpoint: it counts calls and answers with status
// and usage, which tests change.
type fakeAPI struct {
	calls  atomic.Int32
	mu     sync.Mutex
	status int
	usage  *OAuthUsageResponse
}

func (f *fakeAPI) set(status int, usage *OAuthUsageResponse) {
	f.mu.Lock()
	f.status, f.usage = status, usage
	f.mu.Unlock()
}

// env isolates a test: its own TMPDIR, Claude config (logged in as
// account), token, endpoint and clock. It returns the endpoint, the clock
// to move, and a function that logs in as another account.
func env(t *testing.T, account string) (*fakeAPI, *time.Time, func(string)) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	login := func(a string) {
		data := fmt.Sprintf(`{"oauthAccount":{"accountUuid":%q,"emailAddress":"x@example.com","organizationUuid":"org"}}`, a)
		if err := os.WriteFile(filepath.Join(cfg, ".claude.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	login(account)

	api := &fakeAPI{status: 500}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.calls.Add(1)
		api.mu.Lock()
		status, usage := api.status, api.usage
		api.mu.Unlock()
		w.WriteHeader(status)
		if usage != nil {
			_ = json.NewEncoder(w).Encode(usage)
		}
	}))
	t.Cleanup(srv.Close)
	old := usageEndpoint
	usageEndpoint = srv.URL
	t.Cleanup(func() { usageEndpoint = old })

	clock := base
	oldNow := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = oldNow })
	return api, &clock, login
}

func pct(r *ResolvedRateLimit) float64 {
	if r == nil {
		return -1
	}
	return r.Percentage
}

// ---------------------------------------------------------------------------
// A 组：观测合并（mergeObservations）
// ---------------------------------------------------------------------------

func TestMerge_SameWindowTakesHigherUsage(t *testing.T) {
	w := base.Add(3 * time.Hour)
	if got := mergeObservations(obs(40, w), obs(70, w.Add(time.Second)), base); got.Percentage != 70 {
		t.Fatalf("got %v, want 70: usage only grows within a window", got.Percentage)
	}
	if got := mergeObservations(obs(70, w), obs(40, w), base); got.Percentage != 70 {
		t.Fatalf("got %v, want 70 whichever comes first", got.Percentage)
	}
}

func TestMerge_LaterWindowWinsWhateverItsUsage(t *testing.T) {
	// A reset happened: the new window's 5% replaces the old window's 90%.
	// (Taking the max across windows would lock a constraint that no
	// longer exists on the screen.)
	old, next := obs(90, base.Add(10*time.Minute)), obs(5, base.Add(5*time.Hour))
	if got := mergeObservations(old, next, base); got != next {
		t.Fatalf("got %+v, want the later window", got)
	}
	if got := mergeObservations(next, old, base); got != next {
		t.Fatalf("got %+v, want the later window in either order", got)
	}
}

func TestMerge_PastResetIsDropped(t *testing.T) {
	past := obs(90, base.Add(-time.Minute))
	if got := mergeObservations(past, nil, base); got != nil {
		t.Fatalf("got %+v, want nil: the window is over", got)
	}
	cur := obs(3, base.Add(time.Hour))
	if got := mergeObservations(past, cur, base); got != cur {
		t.Fatalf("got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// B 组：resolveRateLimits（共享、预算、账号）
// ---------------------------------------------------------------------------

// Session B sees what session A saw: they share observations through the
// file, without asking the endpoint.
func TestResolve_SessionsShareWhatTheySee(t *testing.T) {
	api, _, _ := env(t, "acct-a")
	w := base.Add(3 * time.Hour)
	resolveRateLimits(makeStdin(stdinBucket(70, w), stdinBucket(30, base.Add(72*time.Hour))))
	five, week := resolveRateLimits(makeStdin(stdinBucket(10, w), stdinBucket(12, base.Add(72*time.Hour))))
	if pct(five) != 70 || pct(week) != 30 {
		t.Fatalf("session B sees 5h %v weekly %v, want 70 and 30", pct(five), pct(week))
	}
	if n := api.calls.Load(); n != 1 {
		t.Fatalf("%d endpoint calls, want 1 (the first, due)", n)
	}
}

// A session left idle past its reset shows the window as not started,
// not its old usage.
func TestResolve_IdleSessionPastResetShowsNothing(t *testing.T) {
	_, clock, _ := env(t, "acct-a")
	resolveRateLimits(makeStdin(stdinBucket(88, base.Add(time.Hour))))
	*clock = base.Add(2 * time.Hour)
	five, _ := resolveRateLimits(makeStdin(stdinBucket(88, base.Add(time.Hour))))
	if five != nil {
		t.Fatalf("5h %+v after its reset, want nil (renders as —)", five)
	}
}

// The endpoint sees usage no session saw (claude.ai, another machine).
func TestResolve_EndpointTopsUp(t *testing.T) {
	api, _, _ := env(t, "acct-a")
	w := base.Add(3 * time.Hour)
	api.set(200, &OAuthUsageResponse{FiveHour: oauthBucket(80, w)})
	five, _ := resolveRateLimits(makeStdin(stdinBucket(40, w)))
	if pct(five) != 80 {
		t.Fatalf("5h %v, want the endpoint's 80", pct(five))
	}
}

// Status lines refresh every 30s in each session. The endpoint allows each
// account ~30 requests an hour, shared with every other tool: ccbar may
// spend at most one every 10 minutes, however many sessions run.
func TestResolve_EndpointAtMostEveryTenMinutes(t *testing.T) {
	api, clock, _ := env(t, "acct-a")
	w := base.Add(5 * time.Hour)
	api.set(200, &OAuthUsageResponse{FiveHour: oauthBucket(10, w)})
	for i := 0; i <= 60; i++ { // 30 minutes, three sessions every 30s
		*clock = base.Add(time.Duration(i) * 30 * time.Second)
		for range 3 {
			resolveRateLimits(makeStdin(stdinBucket(5, w)))
		}
	}
	if n := api.calls.Load(); n != 4 { // at 0, 10, 20 and 30 minutes
		t.Fatalf("%d endpoint calls in 30 minutes, want 4", n)
	}
}

func TestResolve_RateLimitedBacksOffFifteenMinutes(t *testing.T) {
	api, clock, _ := env(t, "acct-a")
	api.set(429, nil)
	for i := 0; i <= 40; i++ { // 20 minutes
		*clock = base.Add(time.Duration(i) * 30 * time.Second)
		resolveRateLimits(makeStdin(stdinBucket(5, base.Add(5*time.Hour))))
	}
	if n := api.calls.Load(); n != 2 { // at 0 and 15 minutes
		t.Fatalf("%d endpoint calls in 20 minutes of 429, want 2", n)
	}
}

func TestResolve_FailureRetriesSoon(t *testing.T) {
	api, clock, _ := env(t, "acct-a")
	api.set(500, nil)
	for i := 0; i <= 10; i++ { // 5 minutes
		*clock = base.Add(time.Duration(i) * 30 * time.Second)
		resolveRateLimits(nil)
	}
	if n := api.calls.Load(); n != 3 { // at 0, 2 and 4 minutes
		t.Fatalf("%d endpoint calls in 5 minutes of 500, want 3", n)
	}
}

// Another session holding the lock is calling: this one neither calls nor
// pushes the schedule back.
func TestResolve_LockHeldSkips(t *testing.T) {
	api, _, _ := env(t, "acct-a")
	api.set(200, &OAuthUsageResponse{})
	if !acquireLock(limitsLock()) {
		t.Fatal("lock")
	}
	resolveRateLimits(nil)
	if n := api.calls.Load(); n != 0 {
		t.Fatalf("%d calls with the lock held", n)
	}
	releaseLock(limitsLock())
	resolveRateLimits(nil)
	if n := api.calls.Load(); n != 1 {
		t.Fatalf("%d calls after the lock went, want 1", n)
	}
}

// Limits are an account's: after switching accounts (claude-swap), the
// last account's usage must not show.
func TestResolve_AccountsDoNotMix(t *testing.T) {
	api, clock, login := env(t, "acct-a")
	w := base.Add(3 * time.Hour)
	api.set(200, &OAuthUsageResponse{FiveHour: oauthBucket(90, w)})
	if five, _ := resolveRateLimits(nil); pct(five) != 90 {
		t.Fatalf("account A 5h %v", pct(five))
	}
	login("acct-b")
	*clock = base.Add(time.Minute)
	api.set(200, &OAuthUsageResponse{FiveHour: oauthBucket(5, base.Add(4*time.Hour))})
	if five, _ := resolveRateLimits(nil); pct(five) != 5 {
		t.Fatalf("account B 5h %v, want its own 5", pct(five))
	}
	login("acct-a")
	if five, _ := resolveRateLimits(nil); pct(five) != 90 {
		t.Fatalf("back on account A 5h %v, want 90", pct(five))
	}
}

func TestResolve_CorruptStoreStartsOver(t *testing.T) {
	_, _, _ = env(t, "acct-a")
	if err := os.WriteFile(limitsPath(), []byte(`{"accounts": {"acct-a/org": {"five_hour": {"pct": 9`), 0o600); err != nil {
		t.Fatal(err)
	}
	five, _ := resolveRateLimits(makeStdin(stdinBucket(12, base.Add(time.Hour))))
	if pct(five) != 12 {
		t.Fatalf("5h %v, want the session's 12", pct(five))
	}
}

// Weekly aggregates within a source (max of the seven-day buckets), then
// merges across sources per window.
func TestResolve_WeeklySameSourceMaxThenMerge(t *testing.T) {
	api, _, _ := env(t, "acct-a")
	w := base.Add(72 * time.Hour)
	api.set(200, &OAuthUsageResponse{SevenDay: oauthBucket(20, w), SevenDayOpus: oauthBucket(35, w)})
	_, week := resolveRateLimits(makeStdin(nil, stdinBucket(30, w)))
	if pct(week) != 35 {
		t.Fatalf("weekly %v, want 35", pct(week))
	}
}

// ---------------------------------------------------------------------------
// C 组：并发原子写
// ---------------------------------------------------------------------------

func TestWriteCacheAtomic_ConcurrentReadersSeeValidJSON(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "ccbar-limits.json")

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

// Housekeeping removes orphan temp files and long-gone sessions' caches,
// leaves fresh files and the shared limits alone, and lists the directory
// at most once per housekeepEvery.
func TestHousekeep(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t0 := time.Now()
	plant := func(name string, age time.Duration) string {
		p := cachePath(name)
		_ = os.WriteFile(p, []byte("{}"), 0o600)
		_ = os.Chtimes(p, t0.Add(-age), t0.Add(-age))
		return p
	}
	freshTmp := plant(tmpCachePrefix+"fresh.json", 0)
	oldTmp := plant(tmpCachePrefix+"old.json", 2*oauthLockTTL)
	liveGit := plant("git-live.json", time.Hour)
	goneGit := plant("git-gone.json", staleCacheAge+time.Hour)
	goneConfig := plant("config-gone.json", staleCacheAge+time.Hour)
	limits := plant("limits.json", staleCacheAge+time.Hour)

	housekeep(t0)
	for _, p := range []string{freshTmp, liveGit, limits} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s removed: %v", filepath.Base(p), err)
		}
	}
	for _, p := range []string{oldTmp, goneGit, goneConfig} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s kept: err=%v", filepath.Base(p), err)
		}
	}

	// Within the hour the next render skips housekeeping.
	again := plant("git-gone-again.json", staleCacheAge+time.Hour)
	housekeep(t0.Add(housekeepEvery / 2))
	if _, err := os.Stat(again); err != nil {
		t.Errorf("housekept again within %v: %v", housekeepEvery, err)
	}
	housekeep(t0.Add(housekeepEvery + time.Minute))
	if _, err := os.Stat(again); !os.IsNotExist(err) {
		t.Errorf("not housekept after %v: err=%v", housekeepEvery, err)
	}
}
