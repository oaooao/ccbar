# Rate Limit Refresh — 设计 Spec

状态：v2（Codex 第二轮 review 反馈已吸收，待 approval）
作者：Leo（Claude）+ Codex 交叉评审
关联代码：`oauth.go`、`types.go`、`main.go`（渲染侧不改）

## v2 相对 v1 的主要修订

- `getOAuthUsage` 返回值从 tuple 收敛为 `oauthResult` struct，新增 `reason` 字段用于 debug / 测试
- 明确**双向契约**：`usage == nil ↔ oauthUnavailable`
- 明确 "bucket 非 nil" 定义为**"能成功解析成 `ResolvedRateLimit`"**
- Cache 原子写改用 `os.CreateTemp` + rename（避免固定 `.tmp` 文件名冲突）
- "Stale OAuth + stdin 有 → 选 stdin" 文案改为产品策略表述
- "不跨源取 max" 升级为显式设计原则
- 测试矩阵扩展到 13 个用例，补状态机边界与 parse 失败
- 实施顺序调整：契约引入与 resolver 接入同 commit 原子落地

## 背景

当前 `resolveRateLimits` 存在两个用户可见 bug：

1. 多 session 并发时 rate limit 不同步（session A 消耗配额，session B 的 status line 看不见）
2. 单 session 长时间存活后不刷新（5h 桶 `resets_at` 已过，ccbar 仍显示老百分比）

根因：stdin 优先级高于 OAuth，直接绕过 `$TMPDIR/ccbar-oauth-usage.json` 的 60s 跨进程文件缓存（本应是全局真相源）。

此外审查过程中暴露出两个独立 bug：

3. `resolveRateLimits` 是 all-or-nothing：只要 `fiveHour != nil || sevenDay != nil` 任一存在就 return，导致维度不全时不会向 OAuth 补齐另一维度
4. `cache` 写入非原子（`os.WriteFile` 直接覆盖），并发读可能读到半截 JSON

## 目标

- OAuth usage（经 60s 跨进程文件缓存）成为 rate limit 的**主真相源**
- stdin 在 OAuth 不可用或只有 stale cache 时作为**可信兜底**
- 决策**粒度到单 bucket**，不再 all-or-nothing
- cache 读写安全：不会读到半截 JSON，不会把"配置坏了"伪装成"有数据"

## 非目标

- 不改 stdin schema、`types.go`、渲染层、主题
- 不改 60s TTL、30s lock TTL、5s API timeout 等常量
- 不改 OAuth token 三级 fallback（env → Keychain → credentials.json）
- 不引入持久日志系统（只加一个轻量 debug 开关，默认静默）

## 核心概念：OAuth 状态分类

当前 `getOAuthUsage()` 返回 `*OAuthUsageResponse`，只能表达"有/无"，无法区分数据的可信度。改为显式状态 + 小 struct。

```go
type oauthState int

const (
    oauthFresh       oauthState = iota // cache 未过期 OR live API 刚刚成功
    oauthStale                         // 有旧数据但无法刷新（token 缺失 / 网络失败 / 锁竞争）
    oauthUnavailable                   // 无任何可用数据（首次运行且 token 缺失/API 挂）
)

type oauthReason string

// 这些是 oauthResult.reason 的合法取值。cache 损坏不在此列——
// 它由独立的 debug 诊断字段 cacheCorruptDetected 表达，见 Debug 开关小节。
const (
    reasonCacheHit      oauthReason = "cache_hit"
    reasonAPIOk         oauthReason = "api_ok"
    reasonLockHeld      oauthReason = "lock_held"
    reasonTokenMissing  oauthReason = "token_missing"
    reasonAPIFailed     oauthReason = "api_failed"
)

type oauthResult struct {
    usage  *OAuthUsageResponse
    state  oauthState
    reason oauthReason // 仅供 debug/测试，不参与业务决策
}

func getOAuthUsage() oauthResult
```

### 双向契约（不变量）

| state | usage |
|---|---|
| `oauthFresh` | **必须非 nil** |
| `oauthStale` | **必须非 nil** |
| `oauthUnavailable` | **必须为 nil** |

违反契约是 bug。`resolveRateLimits` 的实现可以依赖此不变量而无需防御式判 nil。

### `reason` 的定位

`reason` 是**非业务字段**，只服务于 debug log 和测试断言。决策表完全不读它——state 已足够做选源。`oauthState` 只承担选源语义，错误原因由 `reason` 单独暴露。

### 状态判定规则

| 场景 | state | reason | usage |
|---|---|---|---|
| Cache 文件存在且 `age < 60s` | `oauthFresh` | `cache_hit` | cached |
| Cache 过期/缺失，抢锁成功，live API 200 | `oauthFresh` | `api_ok` | fresh |
| Cache 过期/缺失，锁被占 + 旧 cache 存在 | `oauthStale` | `lock_held` | cached |
| Cache 过期/缺失，抢到锁，token 缺失 + 旧 cache 存在 | `oauthStale` | `token_missing` | cached |
| Cache 过期/缺失，抢到锁，live API 失败 + 旧 cache 存在 | `oauthStale` | `api_failed` | cached |
| Cache 过期/缺失，锁被占 + **无**旧 cache | `oauthUnavailable` | `lock_held` | nil |
| Cache 过期/缺失，抢到锁，token 缺失 + **无**旧 cache | `oauthUnavailable` | `token_missing` | nil |
| Cache 过期/缺失，抢到锁，live API 失败 + **无**旧 cache | `oauthUnavailable` | `api_failed` | nil |

**Cache 损坏的权威规则**：`readOAuthCache` 解析失败时**视同无 cache**，走对应的"无 cache"分支（`lock_held` / `token_missing` / `api_failed` + `oauthUnavailable`，或 `api_ok` + `oauthFresh` 若刷新成功）。损坏的 cache 绝不作为 stale 数据返回——`oauthStale` 的 usage 必须是解析成功的真实数据。

`reasonCacheCorrupt` 仅用于 debug 日志补充标注"本次启动发现了 corrupt cache"，不作为最终 `oauthResult.reason` 返回（最终 reason 由后续刷新分支决定）。实现层面：解析失败时可先记一条 debug log，然后按无 cache 路径继续。

**关键约束**：`oauthStale` 必须有解析成功的真实数据才成立。token 缺失且无任何历史 cache 不伪装成 stale——直接 `oauthUnavailable` 让 stdin 接管。

## 决策表：per-bucket 选源

`resolveRateLimits` 拆为两个独立 resolver：`resolveFiveHour` 和 `resolveWeekly`。每个桶独立选源。

### "bucket 可用" 的严格定义

决策表里的 "O 非 nil / S 非 nil" 一律指**"该源该 bucket 能成功解析成 `ResolvedRateLimit`"**，不是原始指针非 nil。具体：

- OAuth 侧：`OAuthRateLimit` 指针非 nil **且** `resets_at`（RFC3339 字符串）`time.Parse` 成功
- stdin 侧：`RateLimit` 指针非 nil（`resets_at` 是 Unix epoch float，解析不会失败）

parse 失败的 bucket 视同 nil，让 fallback 规则接手。这条约束保证测试与实现不会在 parse error 上分叉。

### 决策矩阵

对每个 bucket（记 `O` = OAuth 解析成功的该 bucket，`S` = stdin 解析成功的该 bucket）：

| OAuth state | O 可用 | S 可用 | 选择 | 备注 |
|---|---|---|---|---|
| `oauthFresh` | yes | any | **O** | OAuth 赢 |
| `oauthFresh` | no | yes | **S** | OAuth 该桶空，stdin 补齐 |
| `oauthFresh` | no | no | nil | |
| `oauthStale` | yes | yes | **S** | 见下面"产品策略" |
| `oauthStale` | yes | no | **O** | stale 总比没有强 |
| `oauthStale` | no | yes | **S** | |
| `oauthStale` | no | no | nil | |
| `oauthUnavailable` | — | yes | **S** | 契约保证 O 此时恒为 nil |
| `oauthUnavailable` | — | no | nil | |

### 关键产品策略（不是事实命题）

**Stale OAuth + stdin 可用 → 选 stdin** 是一条**产品权衡**，不是"stdin 一定比 stale cache 新"的事实判断。真实情况下 stdin 也可能比 stale cache 老。这条规则的意图是：

> 当 OAuth 已不可信（已过 TTL 且无法刷新）时，优先当前 session 的本地快照，**牺牲跨 session 一致性来换本地响应性**。

接受的缺陷：此时 session A 和 session B 各看各的 stdin，暂时不同步。可接受，因为此时 OAuth 本来就无法提供跨 session 真相。当 OAuth 恢复 fresh 时，两者自动汇合。

### Weekly bucket 的聚合原则

Weekly 是三桶（`seven_day` / `seven_day_opus` / `seven_day_sonnet`）取 max utilization。

**设计原则：同源自洽快照优先于跨源保守最坏值。**

- 选 OAuth 源时：对 OAuth 的三桶取 max
- 选 stdin 源时：对 stdin 的三桶取 max
- **禁止跨源取 max**

否决跨源 max 的理由：reset 刚发生后 OAuth 已回到低利用率，stdin 可能还是旧高值，跨源 max 会把一个**已经不存在的 binding constraint** 锁在屏幕上。这里优化的是"同一时刻、同一来源的自洽快照"，不是"保守地显示最坏值"。这条原则要明确写在代码注释里，防止后人误改回跨源 max。

### Fresh OAuth 缺桶时 stdin 补齐的意图

实际场景：Max 订阅 `seven_day_opus` 恒为 null 是官方 schema 特性，OAuth 和 stdin 都是 nil，不会触发这行。保留此规则主要是**防御未来 schema 漂移**（某 tier 的 OAuth 返回了 null 但 stdin 仍有旧值时，用 stdin 补齐比丢掉好）。

## 接口契约

### `getOAuthUsage()` 改造

```go
// 返回 oauthResult{usage, state, reason}。
// 调用者只读 state 做选源决策；reason 仅供 debug/测试。
//
// 双向契约（不变量）：
//   state == oauthUnavailable  iff  usage == nil
//   state == oauthFresh || oauthStale  ⇒  usage != nil
//
// 违反契约是 bug，调用方可依赖此不变量免除防御式判 nil。
func getOAuthUsage() oauthResult
```

行为细化：

1. 读 cache 文件；若解析失败视同无 cache（标记 `reasonCacheCorrupt`）
2. 若 cache 解析成功且 `age < 60s` → `{cached, oauthFresh, cache_hit}`
3. 否则尝试非阻塞抢锁 + 刷新：
   - 锁被占 + 有可用 cached → `{cached, oauthStale, lock_held}`
   - 锁被占 + 无 cached → `{nil, oauthUnavailable, lock_held}`
   - 抢到锁 + token 缺失 + 有 cached → `{cached, oauthStale, token_missing}`
   - 抢到锁 + token 缺失 + 无 cached → `{nil, oauthUnavailable, token_missing}`
   - 抢到锁 + API 失败 + 有 cached → `{cached, oauthStale, api_failed}`
   - 抢到锁 + API 失败 + 无 cached → `{nil, oauthUnavailable, api_failed}`
   - 抢到锁 + API 200 → 原子写 cache，`{fresh, oauthFresh, api_ok}`

### `resolveRateLimits()` 改造

```go
func resolveRateLimits(input *StatusInput) (fiveHour, sevenDay *ResolvedRateLimit) {
    r := getOAuthUsage()
    fiveHour = resolveFiveHour(input, r)
    sevenDay = resolveWeekly(input, r)
    if debugEnabled() {
        logDecision(r, fiveHour, sevenDay)
    }
    return
}

func resolveFiveHour(input *StatusInput, r oauthResult) *ResolvedRateLimit
func resolveWeekly(input *StatusInput, r oauthResult) *ResolvedRateLimit
```

每个 resolver 内部：
1. 对 OAuth 侧调用一个 `parseOAuthBucket(*OAuthRateLimit) *ResolvedRateLimit`，parse 失败返回 nil
2. 对 stdin 侧调用 `parseStdinBucket(*RateLimit) *ResolvedRateLimit`
3. 按决策表选源

### Cache 原子写

现在：
```go
_ = os.WriteFile(cacheFile, data, 0644)
```

改成 `os.CreateTemp` + rename（同目录 `filepath.Dir(cacheFile)` 天然同文件系统，不需显式校验）：

```go
dir := filepath.Dir(cacheFile)
f, err := os.CreateTemp(dir, "ccbar-oauth-tmp-*.json")
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
```

`CreateTemp` 的随机后缀保证多 writer 并发时不会共享同一 tmp 名互相踩。`rename(2)` 在同文件系统下原子。

## Debug 开关

新增环境变量 `CCBAR_DEBUG`，非空即开（接受 `1` / `true` / `yes` / 任何非空值），开启后在 stderr 打一行 JSON 记录决策路径：

```json
{"ts":"...","oauth_state":"fresh|stale|unavailable","oauth_reason":"cache_hit|api_ok|lock_held|token_missing|api_failed","cache_corrupt_detected":false,"five_hour_src":"oauth|stdin|none","weekly_src":"oauth|stdin|none"}
```

`oauth_reason` 的枚举与 `oauthResult.reason` 严格一致，不含 `cache_corrupt`。`cache_corrupt_detected` 是独立布尔诊断字段——本次启动发现 corrupt cache 时置 true，与最终 reason 解耦。

判定逻辑：

```go
func debugEnabled() bool { return os.Getenv("CCBAR_DEBUG") != "" }
```

默认不打任何东西（CC status line 对 stderr 敏感）。不引入 logger 库。

## 不改的东西（显式列出防止 scope creep）

- `oauthCacheTTL = 60s`、`oauthLockTTL = 30s`、`apiTimeout = 5s` — 保持
- `getOAuthToken()` 三级 fallback 顺序 — 保持
- `pickWeeklyFromStdin` / `pickWeeklyFromOAuth` 内部聚合逻辑 — 保持
- `ResolvedRateLimit` 结构 — 保持
- `types.go` 全部字段 — 保持
- 渲染层 `render.go` — 保持

## 验证

### 单元测试（新增）

分两组测试，边界清晰：

#### A 组：`resolveFiveHour` / `resolveWeekly` 决策表（策略层）

用 fixture 直接构造 `oauthResult`，断言选源正确：

1. Fresh OAuth + stdin 有 → 选 OAuth
2. Fresh OAuth 该桶 nil + stdin 有 → 选 stdin（防御 schema 漂移）
3. Fresh OAuth 该桶 nil + stdin 无 → nil
4. Stale OAuth + stdin 有 → 选 stdin（产品策略：本地响应性）
5. Stale OAuth + stdin 无 → 选 stale OAuth（比 nil 强）
6. Unavailable + stdin 有 → 选 stdin（契约保证 usage 此时为 nil）
7. Unavailable + stdin 无 → nil
8. Weekly 三桶聚合：OAuth 源内取 max，不跨源取 max（构造 OAuth 低值 + stdin 高值的 fresh 场景，断言输出为 OAuth 低值）
9. OAuth bucket 指针非 nil 但 `resets_at` RFC3339 parse 失败 → 该 bucket 视为 nil，走 fallback
10. `fiveHour` 与 `weekly` 的 source trace 不一致时断言不冲突（两个 resolver 独立选源是允许的）

#### B 组：`getOAuthUsage` 状态机（契约层）

通过注入 token / cache / API behavior 构造各分支，断言 `oauthResult` 三元组完全匹配：

11. 锁被占 + 无 cache → `{nil, oauthUnavailable, lock_held}`
12. 抢到锁 + token 缺失 + 有 cache → `{cached, oauthStale, token_missing}`
13. 抢到锁 + API 失败 + 无 cache → `{nil, oauthUnavailable, api_failed}`
14. Cache 文件为半截 JSON → 视同无 cache，走对应 unavailable 或刷新分支；**不返回 stale-with-corrupt-data**
15. 双向契约测试：遍历所有分支，断言 `usage == nil ↔ state == oauthUnavailable`

#### C 组：并发与原子性（集成层）

16. `for i in {1..20}; do ./ccbar < payload.json &; done` 跑完后 `jq . $TMPDIR/ccbar-oauth-usage.json` 始终成功解析（CreateTemp + rename 验证）

### 手工验证

1. **多 session 并发**：开两个 CC session，A 中持续调用 Opus 拉高周用量，观察 B 在 60s 内 status line 反映变化
2. **长 session 不刷新**：保持单 session 挂机，跨过 5h 桶 `resets_at`，观察百分比归零而非卡旧值
3. **OAuth 完全不可用**：临时 unset `CLAUDE_CODE_OAUTH_TOKEN` + 清空 Keychain + 删 credentials.json + 删 `$TMPDIR/ccbar-oauth-usage.json`，确认 stdin 兜底路径渲染正常
4. **API 间歇失败**：mock usage endpoint 返回 500，观察 stale cache + stdin 兜底行为符合决策表
5. **并发写 cache**：`for i in {1..20}; do ./ccbar < payload.json &; done`，确认 cache 文件始终是合法 JSON（temp + rename 验证）

### 开 debug 观察

跑 `CCBAR_DEBUG=1 ccbar < payload.json`，确认 decision log 与决策表一致。

## 实施顺序（建议）

每步独立 commit，方便 bisect。

1. **Cache 原子写**：`os.CreateTemp` + rename 替换 `os.WriteFile`。独立改动，风险最低，可先落
2. **契约 + resolver 原子落地**：同一 commit 内完成
   - 引入 `oauthState` / `oauthReason` / `oauthResult` 类型
   - 改 `getOAuthUsage()` 签名并实现状态判定规则
   - 拆 `resolveRateLimits` → `resolveFiveHour` + `resolveWeekly`，接入决策表
   - 抽 `parseOAuthBucket` / `parseStdinBucket` 辅助函数（parse 失败返回 nil）
   - 这一步必须原子，避免"旧 caller 忽略 state"的半完成状态
3. **Debug 开关**：加 `debugEnabled()` 与 `logDecision()`
4. **单元测试**：A/B/C 三组覆盖
5. **手工验证**：5 个场景（见下节）

## 风险

- **stdin 与 OAuth 数值单位差异**：已在代码注释中确认都是 0-100，不改
- **Weekly 三桶跨 tier 差异**：Max 订阅 `seven_day_opus` 永远 null 是已知事实，决策表不受影响（OAuth 那桶本来就 nil）
- **Debug 开关污染 stderr**：默认关闭，只在显式 opt-in 时写
- **Temp file 残留**：`os.CreateTemp` 每次生成随机名，进程在 Rename 前崩溃会留下 `ccbar-oauth-tmp-*.json` 残留。反复崩溃会累积。缓解：启动时在 `filepath.Dir(cacheFile)` 下 glob `ccbar-oauth-tmp-*.json`，删除 `mtime` 超过 `oauthLockTTL` 的孤儿 tmp 文件（与锁清理同一时间尺度）。实现放在 `getOAuthUsage` 入口，开销可忽略。

  **命名约束**：tmp 前缀 `ccbar-oauth-tmp-` 与正式 cache `ccbar-oauth-usage.json` 严格隔离，glob pattern 不会匹配正式 cache，清理不会误删主数据。

## 开放问题

1. `oauthStale` 且有 cache 时，是否应该**后台异步**触发一次刷新？（当前设计：不异步，下次启动到 TTL 后自然刷新；简单但 stale 窗口可能略长）
2. `getOAuthToken()` 读 Keychain 需要 `security` exec（~10ms 级别）。是否把 token 也做进程间 cache？（当前设计：不 cache token，每次抢到锁才查；简单但每次 TTL 撞线会重复查）

以上两点倾向"暂不做，保持简单"，待真实 profiling 数据触发再改。
