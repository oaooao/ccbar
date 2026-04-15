# ccbar

A beautifully designed status line for [Claude Code](https://docs.anthropic.com/en/docs/claude-code).

Single binary, zero dependencies, ~60ms startup. Built with Go.

<!-- TODO: Add screenshots -->

## What it shows

```
 Opus 4.6 (1M context) │ Axiom │ ⎇ master ~2 │ $3.17 │ 3h17m
 2 memory files · 1 mcp · 4 hooks
 context  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 68%
 5h       ▰▰▰▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱ 13%  ⟳ 1:00pm
 weekly   ▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▰▱▱▱▱▱▱▱▱ 73%  ⟳ Apr 18, 4:00am
```

**Line 1** — Model, project, git branch with staged/modified counts, session cost, duration

**Line 2** — Config stats: CLAUDE.md files loaded, MCP servers, hooks

**Line 3** — Context window usage bar (cyan → yellow at 60% → red at 80%)

**Line 4–5** — Rate limits with progress bars and reset times (blue → yellow at 60% → red at 80%)

## Features

- **Instant rate limits** — OAuth fallback fetches rate limit data from macOS Keychain + Anthropic API, so you see 5h/7d usage immediately without waiting for the first interaction
- **Smart caching** — Git info cached 5s, config stats cached per session, OAuth cached 60s with stale-while-revalidate
- **Color-coded alerts** — Bars shift from calm (cyan/blue) → warning (yellow) → critical (red) as resources deplete
- **Zero dependencies** — Single Go binary, no runtime needed

## Install

### Homebrew (macOS/Linux)

```bash
brew tap oaooao/tap
brew install ccbar
```

### Go

```bash
go install github.com/oaooao/ccbar@latest
```

### Manual

Download the binary for your platform from [Releases](https://github.com/oaooao/ccbar/releases), extract it, and move it to a directory in your `$PATH`.

## Configure

Add this to your Claude Code settings (`~/.claude/settings.json`):

```json
{
  "statusLine": {
    "type": "command",
    "command": "ccbar",
    "refreshInterval": 3
  }
}
```

Restart Claude Code. The status line appears at the bottom after your first interaction.

## Update

```bash
# Homebrew
brew upgrade ccbar

# Go
go install github.com/oaooao/ccbar@latest
```

## How it works

Claude Code pipes JSON session data to the status line command via stdin on every update. ccbar reads it, gathers supplementary data (git info, config stats, rate limits via OAuth if needed), and prints 5 ANSI-colored lines to stdout.

Rate limits require a Claude.ai Pro/Max subscription. ccbar retrieves your OAuth token from the macOS Keychain (or `~/.claude/.credentials.json`) to fetch rate limit data before the first API response.

## License

MIT
