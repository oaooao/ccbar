# ccbar

Claude Code 的 status line 工具，Go 单二进制。

## 发版流程

```bash
cd ~/Workbench/projects/ccbar
# 改代码、commit
git tag vX.Y.Z
git push origin vX.Y.Z
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

goreleaser 自动完成：编译 4 平台二进制 → 上传 GitHub Releases → 更新 homebrew-tap formula。

推 tag 会触发 GitHub Actions 自动发版，一般不用在本机跑 goreleaser。更新 homebrew-tap 用的是部署密钥：`homebrew-tap` 仓库的 deploy key（可写）配 `ccbar` 仓库的 secret `HOMEBREW_TAP_DEPLOY_KEY`（私钥）。2026-09-29 取代了会过期的个人访问令牌 `HOMEBREW_TAP_TOKEN`。要换密钥：本机 `ssh-keygen -t ed25519` 生成一对，公钥 `gh repo deploy-key add <pub> -R oaooao/homebrew-tap --allow-write`，私钥 `gh secret set HOMEBREW_TAP_DEPLOY_KEY -R oaooao/ccbar < <私钥文件>`，然后删掉本地私钥和旧的 deploy key。

## 项目结构

- `main.go` — 入口 + 5 行输出编排
- `types.go` — JSON 结构体
- `render.go` — ANSI 颜色 + bar 渲染 + format helpers
- `git.go` — git 信息：一次 `git status --porcelain=v2 --branch -uno --no-optional-locks`，按目录缓存 5s，失败或超时沿用旧值
- `cache.go` — 缓存目录 `$TMPDIR/ccbar/` + 每小时最多一次的清理（孤儿 tmp、7 天没更新的 git/config 缓存）
- `config.go` — CLAUDE.md/MCP/hooks 计数 + session 级缓存
- `limits.go` — Rate limit 观测合并：stdin 与用量接口的观测按窗口合并（同窗口取高、新窗口替旧、过 reset 丢弃），按账号跨 session 共享；用量接口最多 10 分钟一次，429 退避 15 分钟
- `oauth.go` — OAuth token 链 + 用量接口调用 + lock + 原子写
- `scripts/demo.sh` — 生成截图用的 mock 数据

## 缓存文件

全部在 `$TMPDIR/ccbar/`（`cache.go`）。**渲染路径上不许遍历共享的 `$TMPDIR`**：2026-10 有个测试套件在里面留下 47.9 万个目录，旧版每次渲染都要 `Glob` 一遍整个目录，单次渲染 2–6 秒，45 个 session 的 ccbar 全卡在磁盘 I/O 上，整机负载 20+。目录遍历只能在 `housekeep` 里做，并且只扫 `$TMPDIR/ccbar/`。

- `git-{hash(cwd)}.json` — 5s TTL，同目录的 session 共用
- `config-{sessionID}.json` — session 生命周期
- `limits.json` — 按账号（`~/.claude.json` 的 oauthAccount）存 5h/weekly 观测与下次可调接口的时间，跨 session 共享
- `limits.lock` — 防并发 API 调用
- `tmp-*.json` — 原子写的临时文件
- `housekeep.stamp` — 上次清理时间；清理每小时最多一次

所有子进程（git、security）有 2s 超时（`subprocessTimeout`），状态栏不能被挂住。git 必须带 `--no-optional-locks`：几十个 session 同时刷新时，否则会抢走 agent 自己 `git commit` 要用的 `index.lock`。

用量接口每个账号每小时约 28–30 次（claude-swap 实测，见其 `poll_policy.py`），与 claude-swap 等工具共用。ccbar 曾经每 60 秒调一次，把额度用光，让 claude-swap 整天 429：别再缩短 `apiRefreshEvery`。

## 分发

- GitHub: https://github.com/oaooao/ccbar
- Homebrew: `brew tap oaooao/tap && brew install ccbar`
- Go: `go install github.com/oaooao/ccbar@latest`
