# ccbar

Claude Code 的 status line 工具，Go 单二进制。

## 发版流程

```bash
cd ~/Code/projects/cli/ccbar
# 改代码、commit
git tag vX.Y.Z
git push origin vX.Y.Z
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

goreleaser 自动完成：编译 4 平台二进制 → 上传 GitHub Releases → 更新 homebrew-tap formula。

## 项目结构

- `main.go` — 入口 + 5 行输出编排
- `types.go` — JSON 结构体
- `render.go` — ANSI 颜色 + bar 渲染 + format helpers
- `git.go` — git 信息 + 5s 文件缓存
- `config.go` — CLAUDE.md/MCP/hooks 计数 + session 级缓存
- `oauth.go` — OAuth token 链 + API 调用 + 60s 缓存 + lock
- `scripts/demo.sh` — 生成截图用的 mock 数据

## 缓存文件

- `/tmp/ccbar-git-{sessionID}` — 5s TTL
- `/tmp/ccbar-config-{sessionID}` — session 生命周期
- `/tmp/ccbar-oauth-usage.json` — 60s TTL，跨 session 共享
- `/tmp/ccbar-oauth.lock` — 防并发 API 调用

## 分发

- GitHub: https://github.com/oaooao/ccbar
- Homebrew: `brew tap oaooao/tap && brew install ccbar`
- Go: `go install github.com/oaooao/ccbar@latest`
