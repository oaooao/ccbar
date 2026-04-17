package main

import "time"

// StatusInput is the JSON structure Claude Code pipes to stdin.
type StatusInput struct {
	Model         Model          `json:"model"`
	Cwd           string         `json:"cwd"`
	SessionID     string         `json:"session_id"`
	SessionName   string         `json:"session_name"`
	Workspace     Workspace      `json:"workspace"`
	Version       string         `json:"version"`
	Cost          *Cost          `json:"cost"`
	ContextWindow ContextWindow  `json:"context_window"`
	RateLimits    *RateLimits    `json:"rate_limits"`
	Vim           *VimMode       `json:"vim"`
	Agent         *AgentInfo     `json:"agent"`
	Worktree      *WorktreeInfo  `json:"worktree"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	CurrentDir  string   `json:"current_dir"`
	ProjectDir  string   `json:"project_dir"`
	AddedDirs   []string `json:"added_dirs"`
	GitWorktree string   `json:"git_worktree"`
}

type Cost struct {
	TotalCostUSD       float64 `json:"total_cost_usd"`
	TotalDurationMs    float64 `json:"total_duration_ms"`
	TotalAPIDurationMs float64 `json:"total_api_duration_ms"`
	TotalLinesAdded    int     `json:"total_lines_added"`
	TotalLinesRemoved  int     `json:"total_lines_removed"`
}

type ContextWindow struct {
	TotalInputTokens    int      `json:"total_input_tokens"`
	TotalOutputTokens   int      `json:"total_output_tokens"`
	ContextWindowSize   int      `json:"context_window_size"`
	UsedPercentage      *float64 `json:"used_percentage"`
	RemainingPercentage *float64 `json:"remaining_percentage"`
}

type RateLimits struct {
	FiveHour       *RateLimit `json:"five_hour"`
	SevenDay       *RateLimit `json:"seven_day"`
	SevenDayOpus   *RateLimit `json:"seven_day_opus"`
	SevenDaySonnet *RateLimit `json:"seven_day_sonnet"`
}

type RateLimit struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       float64 `json:"resets_at"` // Unix epoch seconds
}

type VimMode struct {
	Mode string `json:"mode"`
}

type AgentInfo struct {
	Name string `json:"name"`
}

type WorktreeInfo struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Branch         string `json:"branch"`
	OriginalCwd    string `json:"original_cwd"`
	OriginalBranch string `json:"original_branch"`
}

// OAuthUsageResponse is the shape returned by the Anthropic usage API.
type OAuthUsageResponse struct {
	FiveHour       *OAuthRateLimit `json:"five_hour"`
	SevenDay       *OAuthRateLimit `json:"seven_day"`
	SevenDayOpus   *OAuthRateLimit `json:"seven_day_opus"`
	SevenDaySonnet *OAuthRateLimit `json:"seven_day_sonnet"`
}

type OAuthRateLimit struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"` // ISO 8601
}

// ResolvedRateLimit is the unified internal representation.
type ResolvedRateLimit struct {
	Percentage float64
	ResetsAt   time.Time
}

type GitInfo struct {
	Branch   string
	Staged   int
	Modified int
}

type ConfigStats struct {
	ClaudeMdCount int
	McpCount      int
	HooksCount    int
}
