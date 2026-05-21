# Timezone 架构重构 — Scheduling / Viewing 两层模型

> Status: Implemented
> Last updated: 2026-05-20

## TL;DR

- **问题**：当前代码里 timezone 被三种语义混用，导致 workspace usage 页 picker 在 #2822 review 中被移除（前后端 tz 不一致会把跨 UTC 午夜的行算到错的 calendar week），同时 runtime detail 页的 timezone editor 又承担了"既是物理 tz 又是报表 tz"的双重职责。
- **方案**：把 timezone 收敛成两个独立的 product 概念——**Scheduling**（trigger 规则里写的"9 点"是哪个 9 点，由 `autopilot_trigger.timezone` 承载）和 **Viewing**（用户报表 tz，由新字段 `user.timezone` 承载）。原先混在 `runtime.timezone` 上的"物理位置"语义（Operational）经盘查无真实消费者，整列移除。
- **数据层**：把 `task_usage_daily` (per-runtime, 物化在 runtime tz) 和 `task_usage_dashboard_daily` (workspace 级, 物化在 UTC) **合并成一张 `task_usage_hourly` (UTC, hourly grain)**，所有报表查询按调用方 tz 在查询时切日界。
- **新增字段**：`user.timezone`（默认 = browser detected，可在 Preferences 覆盖）。
- **不引入** `workspace.timezone`——viewing tz 是查看者属性，不是 workspace 属性。
- **性能**：hourly rollup 在密集工况（16 active hours/day）下单 ws 90d 窗口 ~15k 行、~15ms，和现有 daily rollup 同档。
- **副产品**：Migration 082 的"改 runtime tz → 重灌整张 rollup"逻辑可以删除；跨 region 团队自动支持各看各的"今天"；未来要做 hourly heatmap / 时段分析无需再动 schema。

---

## 1. 背景

### 1.1 现状盘点

代码里"timezone"出现在四个地方：

| # | 位置 | 字段 | 实际语义 |
|---|---|---|---|
| 1 | `agent_runtime.timezone` | TEXT, daemon 探测或 UI 覆盖 | 报表 + 物理位置（混淆） |
| 2 | `autopilot_trigger.timezone` | TEXT, 用户写规则时选 | Scheduling（正确） |
| 3 | Workspace Usage 页面 | 无字段，曾在前端用 `useState(browserTimezone())` | Viewing（被 #2822 删除） |
| 4 | 各种 list / log 时间戳显示 | 浏览器 tz | Viewing（隐式） |

### 1.2 问题

**问题 A — Runtime tz 同时承担两个不同的角色：**

`runtime.timezone` 在 migration 082 之后决定了 `task_usage_daily.bucket_date` 的物化口径，等于"报表 tz"；同时 daemon 启动时 `detectLocalTimezone()` 写入这个字段，又当成"机器物理 tz"用。结果：

- 改这个字段会触发整张 rollup 重新物化（migration 082 backfill 逻辑），代价不小。
- 一个 SF 的 dev 把 daemon 跑在 PST 的机器上，但 PM 在上海希望按 CST 出报表——这一个字段没法同时满足两个需求。
- daemon 自动探测的"客观真值"和用户手动想换的"我想看的报表 tz"被同一个 PATCH 接口覆盖，互相打架。

**问题 B — Workspace usage 页面没有正确的"报表 tz"概念：**

PR #2822 删除了 workspace usage 页的 TimezonePicker，原因是：

> 后端 dashboard rollup 把数据按 UTC `bucket_date` 聚合，但前端却驱动 Weekly 边界用用户在 picker 里选的 tz。靠近 UTC 午夜的行会被放进错的 calendar week。Lock workspace Weekly to UTC and remove the timezone picker。

这个修复是对的——前后端 tz 不一致就是 bug。但它**没解决根本问题**：用户确实需要按自己的 tz 看 workspace 报表，只是当前数据层没法支持。

**问题 C — Viewing tz 没有持久化：**

即使 picker 还在，它也只是 `useState(browserTimezone())`——刷新页面、换设备、跨 session 都会丢。用户每次都得手动切。

**问题 D — 没有"跨 region 团队"的支持位：**

把"报表 tz"放在 workspace 上是常见的诱惑，但 workspace 里两个成员一个在 SF 一个在 Beijing，他们想看到的"今天"本来就不同。任何"workspace 级 tz 设置"都强制其中一个人看错位的报表。

### 1.3 目标

1. **架构上清晰**：每个 timezone 字段只回答一个问题。
2. **性能上不退步**：所有现有报表查询保持 <15ms 量级。
3. **正确性优先**：前后端 tz 物化口径必须一致，没有"前端切了但后端没跟"的 UI 谎言。
4. **跨 region 友好**：同一 workspace 不同成员可以各看各的"今天"。

---

## 2. 两个 timezone 概念

| 概念 | 在回答什么 | 谁是真值 | 承载字段 |
|---|---|---|---|
| **Scheduling** | "9 点跑"的 9 点是哪个 9 点 | 用户写规则那一刻的意图 | `autopilot_trigger.timezone` |
| **Viewing** | 我想看的"今天"是哪个日历日 | 当前查看者的偏好 | `user.timezone`（新增） |

**关键论断**：之前代码把"物理位置"和"报表口径"混在 `runtime.timezone` 一个字段上。重构后：

- Scheduling 不动，`autopilot_trigger.timezone` 已经正确。
- Viewing 由新字段 `user.timezone` 承载。
- 数据层不再按任何固定 tz 物化 bucket，而是以 UTC 为唯一存储口径，所有报表查询在 read time 按调用方传入的 tz 切日界。
- `runtime.timezone` 整列删除——见 §2.1。

### 2.1 为什么不要 Operational 层

最初设计有第三个概念 **Operational**（机器物理在哪）。落地盘查后砍掉，两条理由：

**理由一 —— 就算需要 operational tz，`runtime` 也是错的层级。** Operational tz 是**物理机器**的属性，不是 runtime 的属性。同一台机器可以跑多个 runtime，它们共用同一个 OS 时钟，operational tz 必然相同。把 tz 放在 `agent_runtime` 上，等于把一个 machine 级事实复制到同机每一行 runtime——天然的冗余与 drift 风险（同机两个 runtime 的 tz 被改得不一致是无意义的非法状态）。要建模 operational tz，正确归属是 machine 层；而当前 schema 里根本没有 machine 实体，强行放 runtime 层只是把错误固化。

**理由二 —— 它的消费者都不需要 operational 语义。** `runtime.timezone` 今天承担"既是物理 tz 又是报表 tz"的双重职责，但盘查后没有一个读取者真正要"机器物理 tz"：

- runtime detail 页的 Daily / Weekly 趋势图、KPI 卡片，通过 `task_usage_daily` 的物化口径间接吃这个 tz——这是**报表口径**语义，不是 operational。而且这些成本/token 数字要和 workspace dashboard 跨页对账，dashboard 下挂多 runtime、多时区，根本不存在"workspace 的 operational tz"，可对账量只能统一走 Viewing tz。
- hour-of-day heatmap（`GetRuntimeUsageByHour` / `GetRuntimeTaskActivity`）看似要"机器作息"属性，但若只让它一个图表走 operational，用户在同一张卡里切 "Daily" ↔ "Heatmap" 会看到同一个"昨天"两个数。它也只能跟 Viewing tz。

autopilot 调度走 `trigger.timezone` 不碰它，daemon 要时钟直接读 OS clock，`TimezoneEditor` 只是编辑它自己。换句话说，凡是真读它的地方都应当是 Viewing tz——operational 语义在整个系统里没有一个真实需求点。

结论：Operational 作为服务端持久化、用户可编辑的字段没有立足点。机器有物理时钟这个**事实**永远存在，但那是 daemon 进程内部的事，不必上 server。`runtime.timezone` 整列由 migration 104 删除。

代价已知且接受：跨 region 团队看一台 SF runtime 的 hour-of-day heatmap 时，按查看者自己的 tz（如 Asia/Shanghai）显示活跃时段，而非机器本地的 9-to-5。对单 region 团队零影响。

---

## 3. 字段定义与 UI 文案

### 3.1 `runtime.timezone` — 已移除

由 migration `104_drop_runtime_timezone` 删除整列。daemon 注册不再上报 host tz（`detectLocalTimezone()` 删除），`PATCH /api/runtimes/:id` 不再接受 `timezone`（只剩 `visibility`），Runtime Detail 页的 timezone editor 删除。理由见 §2.1。

### 3.2 `autopilot_trigger.timezone` — 不动

已经正确。

### 3.3 `user.timezone` — 新增 Viewing 字段

实现见 migration `100_user_timezone`。表名是 `"user"`（单数、保留字需加引号）：

```sql
ALTER TABLE "user"
    ADD COLUMN timezone TEXT NULL;

COMMENT ON COLUMN "user".timezone IS
    'User-preferred IANA timezone for report rendering (Viewing tz). '
    'NULL means "use the browser-detected tz at render time". Affects '
    'dashboards, charts, and any "today" label shown to this user. Does '
    'not affect data materialisation — all rollups remain in UTC.';
```

`NULL` 是默认值——前端在 NULL 时 fallback 到 `browserTimezone()`。这样新用户零配置就有合理行为。

UI：
- **Settings → Preferences → Timezone**：dropdown，可选 `(browser)` 或具体 IANA name。
- Hint：`"Used for dashboards, charts, and any 'today' label shown to you. Other users in your workspaces will see their own timezone."`

### 3.4 不引入 `workspace.timezone`

理由见 §1.2 问题 D。如果未来真有"workspace 默认报表 tz"的需求（例如新成员加入时给一个建议默认值），可以在那时再加，与本 RFC 兼容——`user.timezone` 可作为 `workspace.timezone` 的 override。

### 3.5 Viewing tz 如何到达后端

报表 handler 通过 `Handler.resolveViewingTZ(r)` 解析当前请求该用哪个 tz 渲染，优先级：

1. `?tz=` query param —— 浏览器端 `useViewingTimezone()` 解析后随每个报表请求显式带上。
2. 已认证用户的 `user.timezone`（query param 缺失时的 cold fallback，会多查一次 `GetUser`）。
3. `"UTC"` —— 兜底。

非法 IANA 名直接跳过该级、不报错（tz 是显示问题）。浏览器走 (1) 显式 query param 这条热路径，旧客户端 / API client 漏传时由 (2) 服务端读 `user.timezone` 兜底。Handler 拿到 tz 后用 `parseSinceParamInTZ` 把 `days=N` 折算成"查看者本地第 N 天零点"对应的 UTC 瞬间，再连同 `@tz` 一起传给 SQL。

---

## 4. 数据层设计

### 4.1 新表 `task_usage_hourly`

实现见 migration `101_task_usage_hourly_schema`（建表）：

```sql
CREATE TABLE task_usage_hourly (
    bucket_hour         TIMESTAMPTZ NOT NULL,   -- UTC, truncated to hour boundary
    workspace_id        UUID        NOT NULL,
    runtime_id          UUID        NOT NULL,
    agent_id            UUID        NOT NULL,
    project_id          UUID,                   -- nullable
    provider            TEXT        NOT NULL,
    model               TEXT        NOT NULL,
    input_tokens        BIGINT      NOT NULL DEFAULT 0,
    output_tokens       BIGINT      NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens  BIGINT      NOT NULL DEFAULT 0,
    task_count          BIGINT      NOT NULL DEFAULT 0,  -- COUNT(DISTINCT task_id)
    event_count         BIGINT      NOT NULL DEFAULT 0,  -- COUNT(*) of task_usage rows
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_usage_hourly_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)
);

CREATE INDEX idx_task_usage_hourly_workspace_time
    ON task_usage_hourly (workspace_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_runtime_time
    ON task_usage_hourly (runtime_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_workspace_agent_time
    ON task_usage_hourly (workspace_id, agent_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_workspace_project_time
    ON task_usage_hourly (workspace_id, project_id, bucket_hour DESC)
    WHERE project_id IS NOT NULL;
```

**关于字段的几个落地决定**：

- **没有 `cost_micros` 列**。成本不在数据层物化——`task_usage_hourly` 只存 token 计数，PK 里带 `provider`+`model`，客户端按 per-model 定价表算成本。这样定价表更新无需重灌 rollup。
- **`task_count` 与 `event_count` 两个计数**：`task_count` 是 `COUNT(DISTINCT task_id)`，`event_count` 是 `COUNT(*)`（同一 task 多次 usage 事件）。注意 task 跨多个 hour bucket 时 `task_count` 会按小时重复计——面向用户的"任务数"列优先用 `agent_task_queue` 派生的查询（见 §4.2），hourly 表的 `task_count` 仅作信息参考。
- **`runtime_id` 为 `NOT NULL`**：`agent_task_queue.runtime_id` 本身带 `NOT NULL` 约束（migration 004），所有建队列的写入路径（含 quick-create）都会带上 runtime，所以 rollup 永远不会产生 no-runtime 的 bucket。`project_id` 可空是因为任务确实可以不挂 project。

migration 101 同时建了两张配套表：

- `task_usage_hourly_rollup_state` —— 单行 watermark 状态表（与 073/084 的 rollup_state 同形）。
- `task_usage_hourly_dirty` —— 失效队列，承载 `updated_at` watermark 看不到的失效（`task_usage` 的 DELETE、级联 DELETE、`issue.project_id` / `agent_task_queue.runtime_id` 改动导致的重新归属）。**必须配 TTL**，见 §4.4。

**这一张表替换两张现有表**：
- `task_usage_daily` (migration 073, 082) — 含 runtime_id，物化在 runtime tz
- `task_usage_dashboard_daily` (migration 084) — 含 agent_id/project_id，物化在 UTC

合并后 PK 同时包含 runtime / agent / project 三个维度，可以从同一张表派生出所有现有视图。

### 4.2 查询模式

Token 类报表查询从 `task_usage_hourly` 派生，按调用方传入的 `@tz` 在查询时折算日界。**成本不在 SQL 里算**——查询只 `SUM` token 列并保留 `model` 维度，成本由客户端按 per-model 定价表折算（所以按日期分组的查询会保留 `model`，按 agent 分组的也是）。

```sql
-- Workspace dashboard 趋势图 ListDashboardUsageDaily（按 viewer tz 切日，保留 model）
SELECT DATE(bucket_hour AT TIME ZONE @tz::text) AS date,
       model,
       SUM(input_tokens)::bigint       AS input_tokens,
       SUM(output_tokens)::bigint      AS output_tokens,
       SUM(cache_read_tokens)::bigint  AS cache_read_tokens,
       SUM(cache_write_tokens)::bigint AS cache_write_tokens,
       SUM(task_count)::int            AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
  AND (@project_id::uuid IS NULL OR project_id = @project_id)
GROUP BY DATE(bucket_hour AT TIME ZONE @tz::text), model
ORDER BY DATE(bucket_hour AT TIME ZONE @tz::text) DESC, model;

-- Runtime detail 趋势图 ListRuntimeUsage（按 viewer tz 切日，tz 来自 user 不是 runtime）
SELECT DATE(bucket_hour AT TIME ZONE @tz::text) AS date,
       provider, model,
       SUM(input_tokens)::bigint AS input_tokens,
       ...
FROM task_usage_hourly
WHERE runtime_id = $1
  AND bucket_hour >= @since::timestamptz
GROUP BY DATE(bucket_hour AT TIME ZONE @tz::text), provider, model
ORDER BY DATE(bucket_hour AT TIME ZONE @tz::text) DESC, provider, model;

-- Per-agent 视图 ListDashboardUsageByAgent / ListRuntimeUsageByAgent
-- 不按日期分组 → 不需要 @tz，只用 @since 截断（@since 已是 viewer tz 折算后的 UTC 瞬间）。
SELECT agent_id, model,
       SUM(input_tokens)::bigint AS input_tokens,
       ...
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
GROUP BY agent_id, model
ORDER BY agent_id, model;
```

**两类查询不走 `task_usage_hourly`**：

- **Time / Tasks 指标**（dashboard 的"时长 / 任务数"标签页）由独立查询 `ListDashboardRunTimeDaily` / `ListDashboardAgentRunTime` 直接打 `agent_task_queue`，按 `completed_at AT TIME ZONE @tz` 切日——任务时长来自队列的 `started_at`/`completed_at`，不是 token rollup 能表达的。它们同样吃 `@tz`，保证 Tokens/Cost/Time/Tasks 四个标签页的日界一致。
- **Runtime hour-of-day Heatmap**（`GetRuntimeUsageByHour` / `GetRuntimeTaskActivity`）仍直接扫原始 `task_usage` / `agent_task_queue`，按 **viewer tz**（`resolveViewingTZ` 解析出的 `@tz`）做 `EXTRACT(HOUR FROM ... AT TIME ZONE @tz)`。Heatmap 窗口小（单 runtime、近 30/90d），raw 扫描足够快，没有必要从 hourly 表派生。

### 4.3 性能预估

单 workspace 90d 窗口的 `task_usage_hourly` 行数：

| 工况 | 行数估算 | 趋势图查询代价 |
|---|---|---|
| 小（5 agent × 2 model × 2 active hour × 90d） | ~1.8k | <5ms |
| 中（5 agent × 2 model × 8 active hour × 90d） | ~7.2k | <10ms |
| 大（5 agent × 2 model × 16 active hour × 90d） | ~14.4k | ~15ms |
| 巨大（20 agent × 5 model × 16 active hour × 90d） | ~144k | ~50ms |

和现有 daily rollup 在同一档。Leaderboard / per-agent / per-project 视图同样指标。

### 4.4 Rollup worker 改造

现有两张 rollup 表的写入逻辑合并成一条管线，实现见 migration `102_task_usage_hourly_pipeline`（触发器 + 窗口函数 + 失效队列 TTL + pg_cron 调度）：

- 源数据扫描不变（仍然扫 `task_usage` 增量 + 失效队列）。`bucket_hour` 用 `task_usage_hour_bucket(tu.created_at)`（UTC 整点截断）。
- Upsert 目标从两张 daily 表改为一张 `task_usage_hourly`。
- 失效队列维度由 `(bucket_date, …)` 改为 `(bucket_hour, …)`（`task_usage_hourly_dirty`），由 `task_usage` / `agent_task_queue` / `issue` 上的触发器写入。**必须配 TTL（保留 7 天）**，否则脏行在密集工况下无界增长——这是整个设计最容易漏的正确性要求（hourly 粒度把脏面比 daily 放大了 ~24×）。
- 调度入口 `rollup_task_usage_hourly()` 由 pg_cron 周期触发：取 advisory lock → 从 `task_usage_hourly_rollup_state` 读 watermark → 调 `rollup_task_usage_hourly_window(from, to)` 重算脏 bucket → 推进 watermark → 释放锁后跑 `prune_task_usage_hourly_dirty()`。单 tick 窗口上限 1 天，watermark 落后时分多次 tick 追平，不会一条语句锁表重算多周。

源表扫描是 worker 的主要开销，目标表换粒度只让单 tick 多几十 ms upsert，不会成倍增长。

### 4.5 Migration 082 的副作用消除

当前 `runtime.timezone` 的 PATCH 处理（migration 082 + 现有 handler）会触发该 runtime 的整张 `task_usage_daily` 重新物化——因为 `bucket_date` 含了 tz。

新方案下 `bucket_hour` 永远是 UTC，**`runtime.timezone` 改变不再触发任何数据层操作**。改 tz 立即生效，零 backfill。这同时修掉了：

- 改 tz 期间的 race condition（旧 bucket 还没重灌完，新查询已经按新 tz 渲染）。
- daemon 第一次注册时探测到非 UTC 的 tz 但历史 rollup 还是 UTC 的尴尬过渡期。

---

## 5. UI / UX 影响

### 5.1 Runtime Detail 页

| 组件 | 重构前 tz 来源 | 重构后 tz 来源 |
|---|---|---|
| Daily / Weekly 趋势图 | `runtime.timezone` | `user.timezone ?? browserTimezone()` |
| KPI 卡片 | `runtime.timezone`（隐式） | `user.timezone ?? browserTimezone()` |
| 日历活跃热力图 | `runtime.timezone` 锚点 + viewer-tz 数据（不一致 bug） | `user.timezone ?? browserTimezone()`（锚点与数据统一） |
| Hour-of-day Heatmap | `runtime.timezone` | `user.timezone ?? browserTimezone()` |
| Timezone editor | 写 `runtime.timezone` | **删除** |

**用户可感知的行为变化**：

- Runtime Detail 页所有图表统一跟随 viewer 自己的 tz；页面上不再有任何 runtime 级 tz 控件。
- 想换报表 tz 的用户去 Settings → Preferences 改一次，所有 workspace / runtime 的报表立刻全跟着变。
- 跨 region 团队：hour-of-day heatmap 按查看者 tz 显示活跃时段（已知且接受的取舍，见 §2.1）。

### 5.2 Workspace Usage 页

恢复"按 viewing tz 渲染"的能力，但**不放页面级 picker**。理由：

- Picker 当年被加上去就是因为没有持久化的 viewing tz 概念。现在有了 `user.timezone`，picker 的诉求被 Preferences 替代。
- 页面级 picker 容易让用户误以为"这是一个 view-state"，但 viewing tz 是全应用属性，不是单页设置。
- 减少 UI 控件 = 减少认知负担。

`packages/views/dashboard/components/dashboard-page.tsx` 里的 `WEEK_TZ = "UTC"` 改成 `useViewingTimezone()`（hook 见 `packages/views/common/use-viewing-timezone.ts`），相应的解释性注释删除。

### 5.3 Preferences 页

新增一个 Timezone setting，和现有的语言 / 主题等并列。

---

## 6. 实施

> 产品尚未上线，无存量用户需保护，全部变更作为一组迁移一次性交付——旧的 daily 管线在同一分支里直接拆除，不保留共存期。

整套变更落在分支 `feat/timezone-architecture`，migration 100–104：

| Migration | 内容 |
|---|---|
| `100_user_timezone` | 加 `"user".timezone` 列（nullable） |
| `101_task_usage_hourly_schema` | 建 `task_usage_hourly` + `task_usage_hourly_rollup_state` + `task_usage_hourly_dirty` + 索引 |
| `102_task_usage_hourly_pipeline` | 失效触发器、`rollup_task_usage_hourly_window` 窗口函数、`prune_task_usage_hourly_dirty()` 失效队列 TTL、带单日 cap 与 prune 的 `rollup_task_usage_hourly()` cron 入口、pg_cron 调度 |
| `103_drop_legacy_daily_rollups` | 拆掉 `task_usage_daily` / `task_usage_dashboard_daily` 两条旧管线（表、函数、触发器、pg_cron 任务） |
| `104_drop_runtime_timezone` | 删除 `agent_runtime.timezone` 列（Operational 层移除，见 §2.1） |

配套的代码侧改动：

- **数据回填**：一次性命令 `cmd/backfill_task_usage_hourly`，按 workspace 切片把历史 `task_usage` 灌进新表。旧的 `cmd/backfill_task_usage_daily` / `cmd/backfill_task_usage_dashboard_daily` 已删除。
- **查询切换**：后端所有报表查询迁到 `task_usage_hourly`（或 Time/Tasks 的 `agent_task_queue` 查询），统一接受 `@tz`；`UseDailyRollupForDashboard` / `UseDailyRollupForRuntimeUsage` 等 feature flag 与旧的 raw-scan / daily-rollup 双查询路径一并删除。
- **前端打通**：`useViewingTimezone()` hook 解析 viewer tz，报表组件随请求带 `?tz=`；`dashboard-page.tsx` 的 `WEEK_TZ = "UTC"` 改为 `useViewingTimezone()`，原 UTC-lock 解释性注释删除。
- **UI 文案**：Preferences 新增 Timezone setting。Runtime Detail 页的 timezone editor 整体删除。
- **runtime tz 移除**：`PATCH /api/runtimes/:id` 的 `timezone` 字段删除，该端点只剩 `visibility`；daemon 注册不再上报 host tz；`agent_runtime.timezone` 列由 migration 104 删除。

---

## 7. Open questions / Risks

### 7.1 Risks

- **Invalidation queue TTL 是必做**。如果忘记加，密集工况下 queue 会无界增长。
- **Hourly rollup backfill 期间的源表 read pressure**。按 workspace 切片、低峰期跑，预期 OK，但需要提前给 DB 团队打招呼。
- **DST 当天的 23h/25h "日"**。`DATE(bucket_hour AT TIME ZONE @tz)` 会正确处理，但前端任何"一天 = 24 小时"的硬编码偏移逻辑要测一遍 DST 边界。
- **现有 `runtime.timezone` 的 PATCH endpoint 行为变了**。改完不再触发 backfill——这是好事，但 API 文档和 changelog 要写清楚，避免下游集成误判。

### 7.2 Open question

- **Trigger 的 timezone 默认值**？目前用户必须手动选；可以默认 `user.timezone`，但用户写 trigger 时的 viewing tz 和 trigger 实际跑的 tz 是两件事，需要产品决策。

### 7.3 非目标

- **不做** workspace 级 tz 设置：跨 region 团队两个成员各自正确的"今天"不同，workspace 级 tz 必让其中一方看错位报表。
- **不做** 预物化多 tz rollup：IANA tz 列表有 ~600 个无法穷举、DST 需逐 tz 维护，而 hourly rollup 已经够快。
- **不做** issue / comment / inbox 等列表的 tz 切换——它们已经隐式用浏览器 tz，本 RFC 不动。后续如果要让这些也跟 `user.timezone`，是独立的 follow-up。

---

## 8. 决策汇总

| 决策点 | 选择 |
|---|---|
| Timezone 概念分层 | Scheduling / Viewing 两层（Operational 经盘查后移除） |
| `runtime.timezone` 角色 | ❌ 整列删除（migration 104） |
| `user.timezone` 是否新增 | ✅ 新增，nullable，默认 fallback 到 browser |
| `workspace.timezone` 是否新增 | ❌ 不引入 |
| 数据层物化口径 | 统一 UTC, hourly grain |
| Rollup 表合并 | `task_usage_daily` + `task_usage_dashboard_daily` → `task_usage_hourly` |
| 报表 tz 切换粒度 | 全局 per-user（Preferences），不做 per-view picker |
| hour-of-day heatmap tz | viewer tz（不再用机器物理 tz） |
