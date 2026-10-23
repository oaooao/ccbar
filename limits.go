package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Rate limits come from two sources: the rate_limits Claude Code pipes to
// each session's status line, which is free but only as recent as that
// session's last request, and the usage endpoint, which also sees other
// sessions, claude.ai and other machines, but allows each account only
// about 28-30 requests an hour (claude-swap measured it, poll_policy.py),
// shared with every other tool asking. Polling it every minute spent the
// whole budget and left claude-swap rate limited all day.
//
// So nothing picks one source over the other. Every value seen, from
// stdin or the endpoint, is an observation of one account's window, and
// observations merge: within a window usage only grows, and a later window
// replaces an earlier one. Two observations merge right without knowing
// which is newer. Sessions share what they see through one file, keyed by
// account, and the endpoint is a rare top-up for usage no session saw.
// See docs/rate-limit-refresh-spec.md, v3.

const (
	// apiRefreshEvery: at most 6 endpoint calls an hour per account.
	apiRefreshEvery = 10 * time.Minute
	// apiBackoff after a 429: its budget frees as old requests age out of
	// the trailing hour, so asking again soon only spends it again.
	apiBackoff = 15 * time.Minute
	// apiRetryAfterFailure: a network error or other status.
	apiRetryAfterFailure = 2 * time.Minute
	// sameWindowSlack: two resets this close are the same window, as the
	// endpoint and stdin round them differently.
	sameWindowSlack = 2 * time.Minute
)

// now is the clock; tests replace it.
var now = time.Now

// observation is a window's usage as one source saw it.
type observation struct {
	Percentage float64   `json:"pct"`
	ResetsAt   time.Time `json:"resets_at"`
}

func (o *observation) resolved() *ResolvedRateLimit {
	if o == nil {
		return nil
	}
	return &ResolvedRateLimit{Percentage: o.Percentage, ResetsAt: o.ResetsAt}
}

func observe(r *ResolvedRateLimit) *observation {
	if r == nil {
		return nil
	}
	return &observation{Percentage: r.Percentage, ResetsAt: r.ResetsAt}
}

// mergeObservations merges two observations of one window at t: one past
// its reset is over and drops; a later window wins; in the same window
// the higher usage does, as usage within a window only grows.
func mergeObservations(a, b *observation, t time.Time) *observation {
	if a != nil && !a.ResetsAt.After(t) {
		a = nil
	}
	if b != nil && !b.ResetsAt.After(t) {
		b = nil
	}
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case b.ResetsAt.Sub(a.ResetsAt) > sameWindowSlack:
		return b
	case a.ResetsAt.Sub(b.ResetsAt) > sameWindowSlack:
		return a
	case b.Percentage > a.Percentage:
		return b
	}
	return a
}

// accountLimits is what every session knows of one account's limits.
type accountLimits struct {
	FiveHour *observation `json:"five_hour,omitempty"`
	Weekly   *observation `json:"weekly,omitempty"`
	// APIAt is when the endpoint was last asked; APINextAt when it may be
	// asked again.
	APIAt     time.Time `json:"api_at,omitzero"`
	APINextAt time.Time `json:"api_next_at,omitzero"`
}

func (l *accountLimits) merge(five, weekly *observation, t time.Time) {
	l.FiveHour = mergeObservations(l.FiveHour, five, t)
	l.Weekly = mergeObservations(l.Weekly, weekly, t)
}

// limitsStore is the file sessions share, by account.
type limitsStore struct {
	Accounts map[string]*accountLimits `json:"accounts"`
}

func limitsPath() string { return cachePath("limits.json") }
func limitsLock() string { return cachePath("limits.lock") }

func loadLimits() *limitsStore {
	s := &limitsStore{}
	if data, err := os.ReadFile(limitsPath()); err == nil {
		_ = json.Unmarshal(data, s) // a corrupt file starts over
	}
	if s.Accounts == nil {
		s.Accounts = map[string]*accountLimits{}
	}
	return s
}

func (s *limitsStore) save() {
	if data, err := json.Marshal(s); err == nil {
		writeCacheAtomic(limitsPath(), data)
	}
}

// account returns the limits of the account key, made if new.
func (s *limitsStore) account(key string) *accountLimits {
	l := s.Accounts[key]
	if l == nil {
		l = &accountLimits{}
		s.Accounts[key] = l
	}
	return l
}

// currentAccount identifies the account Claude Code is logged in as, from
// the oauthAccount of its config: limits are an account's, and switching
// accounts (as claude-swap does) must not show the last one's. "" when
// unknown, which still works for a single account.
func currentAccount() string {
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".claude.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		OAuthAccount struct {
			AccountUUID  string `json:"accountUuid"`
			EmailAddress string `json:"emailAddress"`
			OrgUUID      string `json:"organizationUuid"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	a := cfg.OAuthAccount
	if a.AccountUUID != "" {
		return a.AccountUUID + "/" + a.OrgUUID
	}
	return a.EmailAddress + "/" + a.OrgUUID
}

// stdinObservations reads the session's rate limits.
func stdinObservations(input *StatusInput) (five, weekly *observation) {
	if input == nil || input.RateLimits == nil {
		return nil, nil
	}
	return observe(parseStdinBucket(input.RateLimits.FiveHour)), observe(pickWeeklyFromStdin(input.RateLimits))
}

// oauthObservations reads the endpoint's usage.
func oauthObservations(u *OAuthUsageResponse) (five, weekly *observation) {
	if u == nil {
		return nil, nil
	}
	return observe(parseOAuthBucket(u.FiveHour)), observe(pickWeeklyFromOAuth(u))
}

// apiDue reports whether the endpoint may be asked for the account.
func (l *accountLimits) apiDue(t time.Time) bool {
	return !t.Before(l.APINextAt)
}

// askAPI calls the endpoint once, if no other session is, and schedules
// the next call by the outcome.
func askAPI() (*OAuthUsageResponse, apiOutcome) {
	if !acquireLock(limitsLock()) {
		return nil, apiLockHeld
	}
	defer releaseLock(limitsLock())
	token := getOAuthToken()
	if token == "" {
		return nil, apiNoToken
	}
	return callUsageAPI(token)
}

// nextAPI is when to ask again after an outcome at t.
func nextAPI(o apiOutcome, t time.Time) time.Time {
	switch o {
	case apiOK:
		return t.Add(apiRefreshEvery)
	case apiRateLimited:
		return t.Add(apiBackoff)
	case apiLockHeld:
		return time.Time{} // the session holding it schedules
	}
	return t.Add(apiRetryAfterFailure)
}

// resolveRateLimits merges this session's rate limits into what all
// sessions saw of the account, asks the endpoint when due, and returns the
// 5h and weekly limits; nil for a window not started (or past its reset
// with nothing newer), which renders as "—".
func resolveRateLimits(input *StatusInput) (fiveHour, sevenDay *ResolvedRateLimit) {
	t := now()
	key := currentAccount()
	sFive, sWeekly := stdinObservations(input)

	store := loadLimits()
	l := store.account(key)
	l.merge(sFive, sWeekly, t)

	outcome := apiSkipped
	if l.apiDue(t) {
		var usage *OAuthUsageResponse
		usage, outcome = askAPI()
		// Another session may have written while the endpoint answered.
		store = loadLimits()
		l = store.account(key)
		l.merge(sFive, sWeekly, t)
		oFive, oWeekly := oauthObservations(usage)
		l.merge(oFive, oWeekly, t)
		if outcome != apiLockHeld {
			l.APIAt, l.APINextAt = t, nextAPI(outcome, t)
		}
	}
	store.save()

	if debugEnabled() {
		logDecision(key, outcome, l)
	}
	return l.FiveHour.resolved(), l.Weekly.resolved()
}

// debugEnabled returns true when CCBAR_DEBUG is set to any non-empty value.
func debugEnabled() bool {
	return os.Getenv("CCBAR_DEBUG") != ""
}

// logDecision writes a single JSON line to stderr describing the limits
// resolved. Gated by debugEnabled so the status line never leaks noise.
func logDecision(account string, outcome apiOutcome, l *accountLimits) {
	entry := map[string]any{
		"ts":          now().UTC().Format(time.RFC3339Nano),
		"account":     account,
		"api":         string(outcome),
		"api_next_at": l.APINextAt,
		"five_hour":   l.FiveHour,
		"weekly":      l.Weekly,
	}
	if data, err := json.Marshal(entry); err == nil {
		fmt.Fprintln(os.Stderr, string(data))
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

// pickWeeklyFromStdin aggregates the weekly bucket within the stdin source
// (same-source max). Merging with other sources happens per window after.
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

// pickWeeklyFromOAuth aggregates the weekly bucket within the OAuth source
// (same-source max).
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
