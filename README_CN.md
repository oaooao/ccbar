# ccbar

**[English](README.md) | [中文](README_CN.md)**

为 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 设计的高颜值状态栏。

单二进制，零依赖，~60ms 启动。Go 编写。

### 正常状态

![Normal state](assets/normal.png)

### 警告状态 — 资源开始紧张

![Warning state](assets/warning.png)

### 危险状态 — 该 /save 了

![Critical state](assets/critical.png)

## 显示内容

**第 1 行** — 模型名称、项目名、Git 分支（含 staged/modified 数量）、费用、时长

**第 2 行** — 配置统计：已加载的 CLAUDE.md 文件数、MCP 服务数、Hooks 数

**第 3 行** — Context 窗口使用率进度条（青色 → 60% 黄色 → 80% 红色）

**第 4–5 行** — Rate limit 进度条及重置时间（蓝色 → 60% 黄色 → 80% 红色）

## 特性

- **即时显示 Rate Limit** — 通过 macOS Keychain + Anthropic API 的 OAuth 回退机制获取数据，无需等待首次交互即可看到 5h/7d 用量
- **智能缓存** — Git 信息 5 秒缓存，配置统计按 session 缓存，OAuth 60 秒缓存 + stale-while-revalidate
- **颜色预警** — 进度条随资源消耗自动变色：正常（青/蓝）→ 警告（黄）→ 危险（红）
- **零依赖** — 单个 Go 二进制，不需要任何运行时环境
- **深色/浅色主题** — 针对深色和浅色终端背景分别优化了配色方案
- **日期本地化** — 中文环境自动使用 24 小时制和 `4/18` 格式，英文环境使用 12 小时制和 `Apr 18` 格式

## 安装

### Homebrew（macOS/Linux）

```bash
brew tap oaooao/tap
brew install ccbar
```

### Go

```bash
go install github.com/oaooao/ccbar@latest
```

### 手动安装

从 [Releases](https://github.com/oaooao/ccbar/releases) 下载对应平台的二进制文件，解压后放到 `$PATH` 中的任意目录。

## 配置

在 Claude Code 的配置文件（`~/.claude/settings.json`）中添加：

```json
{
  "statusLine": {
    "type": "command",
    "command": "ccbar",
    "refreshInterval": 3
  }
}
```

重启 Claude Code 即可生效。

### 浅色主题

如果你使用浅色终端背景，添加 `--theme light` 参数：

```json
{
  "statusLine": {
    "type": "command",
    "command": "ccbar --theme light",
    "refreshInterval": 3
  }
}
```

### 语言设置

日期时间格式默认根据系统 `LANG` 环境变量自动检测。如需手动指定：

```json
{
  "statusLine": {
    "type": "command",
    "command": "ccbar --locale zh",
    "refreshInterval": 3
  }
}
```

| 参数 | 时间格式 | 日期格式 |
|------|---------|---------|
| `--locale zh` | `15:00` | `4/18 15:00` |
| `--locale en` | `3:00pm` | `Apr 18, 3:00pm` |
| *（默认）* | 跟随系统 | |

参数可以组合使用：`ccbar --theme light --locale zh`

## 更新

```bash
# Homebrew
brew upgrade ccbar

# Go
go install github.com/oaooao/ccbar@latest
```

## 工作原理

Claude Code 在每次更新时通过 stdin 将 JSON 格式的会话数据传给状态栏命令。ccbar 读取 JSON 后，补充采集 Git 信息、配置统计、Rate Limit（需要时通过 OAuth API 获取），然后输出 5 行带 ANSI 颜色的文本到 stdout。

Rate Limit 功能需要 Claude.ai Pro/Max 订阅。ccbar 从 macOS Keychain（或 `~/.claude/.credentials.json`）获取 OAuth token，在首次 API 响应前就能拉取 Rate Limit 数据。

## 许可证

MIT
