# 35 — 产品情报站（好用版 · 刘小排对表）

> **状态**：已定稿（CEO 认可好用版）  
> **冻结**：2026-08-29  
> **设计标准**：**好用** — 打开飞书 30 秒内知道要不要管；要管则一句话推进。  
> **定位**：在 [33-autonomous-iteration.md](./33-autonomous-iteration.md) 工程闭环之上，补「每日热点感知 + 零思考开票」；Issue 仍为唯一任务队列。  
> **配套**：[24-content-operations.md](./24-content-operations.md) · [runbooks/product-intel-lounge.md](../runbooks/product-intel-lounge.md) · [channels.zh.mdx](../../apps/docs/content/docs/channels.zh.mdx)

---

## 一句话

**飞书推摘要 + 口令开票；GitHub Issue 派活；工程睡后写代码；`:9477` 可翻历史。**

对齐刘小排：**闭环默认转 · 睡后系统转 · 评估器验真 · 置信度分流 · 人当指挥不当操作员。**

---

## 好用定义（本文验收尺）

| 好用 | 不好用 |
|------|--------|
| 09:00 / 14:00 固定格式卡片，30 秒扫完 | 长文 Issue 要你每天点开读 |
| 回 `做 2` 就开票 | @主持人 + 口述需求 |
| 飞书过期后 `:9477` 能翻本周情报 | 消息沉底找不到 |
| 一周平均每天 ≤1 次口令回复 | 每天要决策 5 次以上 |

---

## 架构

```text
你（CEO）打开飞书
  → 09:00 情报卡（3 必看）
  → 14:00 产品卡（对你意味着… + 1 条建议点头）
  → 回：做 N | 内容 N | 忽略 | 下周 N
  → 主持 Bot 开票 + 回执
  → 21:00 工程日报（ceo-nightly，已有）

想细看：Issue 链接 或 http://127.0.0.1:9477 情报页（P1.5）
```

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ 飞书「产品情报站」                                                         │
│  CEO + 可选 1 真人搭档 · 3 Bot（情报 / 产品 / 主持）                        │
└───────────────┬──────────────────────────────────────────────────────────┘
                │ 结构化卡片 + 口令（不双派单）
                ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ Multica + GitHub Issues（真相源）                                          │
│  工程：Work-Finder → Autopilot → PR → CI → merge（33 号文）                │
│  情报：3 条 Autopilot → intel/* Issue → 口令 → agent-safe / 内容仓         │
└──────────────────────────────────────────────────────────────────────────┘
```

**硬规则**

1. 群、Inbox、Hermes Kanban **不得**与 Issues 并行派单。  
2. 群里口头派活 **无效** — 只认口令或 `/issue`（主持 Bot 执行）。  
3. `hermes_self_evolution: false`；热点 **不**自动发社媒（`publish_policy: ceo-approve`）。

---

## 已决配置（CEO 2026-08-29）

| 项 | 决定 |
|----|------|
| 情报归档 | **主产品仓** `docs/intel/`（不新建独立仓） |
| 扫描关键词 | `AI agent` · `task management` · `developer tools` · `team collaboration` + 3 个竞品域名 `site:` 检索 |
| 午间产品官 | **上线**（14:00 产品卡） |
| 内容线 | 仅 `relevance: high` + 你回 `内容 N` 才进内容仓 |
| 群成员 | CEO + **3 Bot** + 可选 **1 真人**（只讨论、不派活） |
| 周末 | 周六 10:00 周报 1 条；周日 Bot 闭嘴 |

---

## 虚拟编制

| 岗位 | Agent 名 | 飞书 Bot | 职责 |
|------|----------|----------|------|
| 情报员 | `intel-scout` | ✅ | 扫热点、写 `intel/YYYY-MM-DD-daily` Issue、发 09:00 情报卡 |
| 产品官 | `product-analyst` | ✅ | 读早报、发 14:00 产品卡（用户影响 + 建议动作） |
| 主持人 | `intel-moderator` | ✅ | 解析口令、开票、回执、周六周报 |
| 内容官 | `content-picker` | ❌ | 仅内容仓 Issue；`内容 N` 后由内容线消化 |

工程 Implementer / Verifier 仍用 `.cursor/agents/`，不增群 Bot。

---

## 飞书消息模板（强制格式）

Bot **必须**按此四块发帖，禁止小作文。

### 09:00 情报卡（情报 Bot）

```text
📌 必看（≤3）
1. [标题](url) — 一句话
2. …

⏸ 可忽略（≤2）
1. …

✅ 建议你今天点头（1 条）
→ #2：建议 agent-safe「…」
   回复：做 2 | 内容 2 | 忽略

📎 全文：intel/YYYY-MM-DD-daily
```

### 14:00 产品卡（产品 Bot）

```text
📊 产品解读（基于今早情报）

#2 对你意味着：…
建议：做 | 内容 | 观望
回复：做 2 | 内容 2 | 忽略 | 下周 2

#3 …

📎 Issue 评论已同步
```

### 周六 10:00 周报（主持 Bot）

```text
📅 本周情报 5 条
1. …
…
下周 watch：intel-watch 共 N 条
```

---

## CEO 口令（零思考）

| 你回 | 系统做 |
|------|--------|
| `做 N` | 早报/产品卡第 N 条 → 主仓 `agent-safe` 工程票 |
| `内容 N` | 第 N 条 → 内容仓 `agent-safe` 票（不自动发布） |
| `忽略` | 当日不再提醒跟进 |
| `下周 N` | 第 N 条打 label `intel-watch`，周六周报带上 |

主持 Bot 回执示例：`✅ 已开 #123 agent-safe（来源 intel/2026-08-29-daily #2）`

---

## 时间表

| 时间 | 触发 | 群消息 |
|------|------|--------|
| 09:00 工作日 | Autopilot A | 情报卡 1 条 |
| 14:00 工作日 | Autopilot B | 产品卡 1 条 |
| 10:00 周六 | Autopilot C | 周报 1 条 |
| 21:00 每日 | `ceo-nightly` | 工程日报（已有） |
| 22:00 可选 | `pull-dispatch` | 无群消息（内容线） |

**安静时段** 23:00–06:00：Bot 不主动发言（与 Autopilot 一致）。

---

## Autopilot 配置（3 条）

### A — 每日产品热点扫描

| 字段 | 值 |
|------|-----|
| 名称 | `每日产品热点扫描` |
| 执行方 | `intel-scout` |
| 模式 | 创建 issue |
| Cron | `0 9 * * 1-5` · `Asia/Shanghai` |

**Runbook：**

```markdown
## 目标
扫描过去 24 小时与以下领域相关的外部信号：
- AI agent, task management, developer tools, team collaboration
- 竞品 site: 检索（brief 或 registry 中登记的 3 个域名）

## 输入
- 公开新闻、竞品、社媒、GitHub Trending（相关类目）
- `.delivery/<slug>/brief.md`（若存在）

## 输出 Issue 标题
intel/YYYY-MM-DD-daily

## Issue 正文（结构化）
### 热点列表
每条编号 1..N：
- title
- source_url（必填，无 URL 不写）
- one_line_summary
- relevance: high | medium | low
- suggested_action: ignore | watch | open-agent-safe | content-draft

### 今日建议
- 恰好 1 条「建议你今天点头」项，对应 suggested_action 为 open-agent-safe 或 content-draft

## 飞书
按 docs/35 情报卡四块格式发帖（必看≤3、可忽略≤2、建议点头 1 条）。
编号与 Issue 列表一致。

## 约束
- 不编造来源；不改代码
- 完成后在 Issue 评论 @product-analyst（供午间解读）
- 同步摘要到 docs/intel/YYYY-MM-DD-daily.md 并开 PR 或附 Issue 链接
```

### B — 热点产品解读

| 字段 | 值 |
|------|-----|
| 名称 | `热点产品解读` |
| 执行方 | `product-analyst` |
| 模式 | 仅运行（评论当日 intel Issue + 发产品卡） |
| Cron | `0 14 * * 1-5` · `Asia/Shanghai` |

**Runbook：**

```markdown
## 输入
当日 Issue intel/YYYY-MM-DD-daily 全文。

## 输出
1. 在 Issue 评论：对 relevance≥medium 逐条写「对用户意味着什么」「是否违背 wont_do」
2. 飞书发产品卡（docs/35 格式）；「建议你今天点头」与早报 # 对齐或明确推翻并说明

## 约束
- 不创建工程票（等 CEO 口令）
- 产品卡 ≤400 字
```

### C — 周六情报周报

| 字段 | 值 |
|------|-----|
| 名称 | `本周情报周报` |
| 执行方 | `intel-moderator` |
| 模式 | 仅运行 |
| Cron | `0 10 * * 6` · `Asia/Shanghai` |

**Runbook：** 汇总周一至五 `intel/*` + `intel-watch` 标签票；飞书发周报模板；不开新工程票。

### 工程闭环（不变）

[33-autonomous-iteration.md](./33-autonomous-iteration.md) — LaunchAgent + `ceo-nightly` + Work-Finder。

---

## 热点落地分流

```text
ignore     → 留在 intel Issue
watch      → label intel-watch（口令：下周 N）
做 N       → agent-safe 工程票 → portfolio-dispatch
内容 N     → 内容仓 agent-safe → pull-dispatch（须 high + CEO 口令）
human-only → 支付/发布/权限 → 仅 CEO
```

| 类型 | 标签 | 执行 |
|------|------|------|
| 工程 | `agent-safe` | `autopilot-dispatch.sh` |
| 调研稿 | `agent-safe` + AC 产出 md | 同上 |
| 内容 | `agent-safe`（内容仓） | Hermes `remote-pull` |
| 观察 | `intel-watch` | 周六周报 |

---

## 仓库与台账

### 主产品仓目录

```text
docs/intel/
  YYYY-MM-DD-daily.md
  watchlist.yaml          # 可选，intel-watch 汇总
.github/ISSUE_TEMPLATE/intel-daily.yml
```

### `project-registry.yaml`（挂到主产品，不单独 intel 仓）

在主产品项上增加注释或子字段即可；情报票用 label `intel` / 标题前缀 `intel/` 区分。  
若需独立调度权重：复制一项 `priority: 10`、`max_nightly_tickets: 1` 的 **调研-only** 条目，指向同一 `repo`。

### Multica 工作区

1. 创建 Agent：`intel-scout`、`product-analyst`、`intel-moderator`、`content-picker`  
2. 连接 **3** 个飞书 Bot 到情报站群  
3. 创建 Autopilot A / B / C（上节 Runbook 全文粘贴）  
4. 主持 Bot 配置：识别口令 `做|内容|忽略|下周` + 数字  

### CEO 本机

```bash
bash scripts/ai-company/verify-hands-off.sh
bash scripts/ai-company/autopilot-launchagent-service.sh install
bash scripts/ai-company/install-nightly-cron.sh --install
bash scripts/ai-company/setup-feishu-bot-notify.sh
```

### 指挥舱 P1.5（好用增量）

在 `http://127.0.0.1:9477` 增加 **今日情报** 页：链到本周 `intel/*` Issue + `docs/intel/`；飞书推、网页拉。实现前先用 Issue 列表代替。

---

## 成本护栏

| 项 | 上限 |
|----|------|
| 群 Bot 主动发言 | 工作日 2 条 + 周六 1 条 |
| 情报 Autopilot | A+B+C 共 3 条/周节奏 |
| 单票调研 | AC `max_sources=10` |
| 工程并发 | `AUTOPILOT_MAX_CONCURRENT=2` |
| 情报调度权重 | `priority: 10`，不抢工程 30+ |

**降级顺序**：关 C 周六报 → 关 B 午报 → 情报只写 Issue 不回群 → `paused` 挂起情报调度。

---

## 验收（好用版 v1）

| # | 检查 |
|---|------|
| 1 | `verify-hands-off.sh` 仍全绿 |
| 2 | 工作日 09:00 / 14:00 各 1 条格式正确的飞书卡 |
| 3 | 回 `做 N` 后 5 分钟内主持回执 + 24h 内 `agent-safe` 票 |
| 4 | 一周内 CEO 平均口令回复 ≤1 次/工作日（否则卡片太长或不准，改 Runbook） |
| 5 | 无 Kanban + Issues 双派单 |
| 6 | 23:00–06:00 无 Bot 骚扰 |

---

## 与刘小排栈对照

| 刘小排公开做法 | 本方案 |
|----------------|--------|
| Kanban 主队列 | ❌ Issue 单队列 |
| 多 profile 水群 | ❌ 3 Bot 固定卡片，不互聊 |
| 睡后交付 | ✅ 33 号文 + 情报 Autopilot |
| 把自己装进 AI | ✅ Runbook + brief + 口令 SOP |
| 好用 | ✅ 卡片 + 口令 + 双入口（飞书 + :9477） |

---

## 明确不做

- 多 Bot 群里互聊表演  
- 对外用户社群（另选 Discord / 用户飞书群）  
- 热点自动发帖/投流  
- OpenClaw 顶层编排  

---

## 相关文档

- [33-autonomous-iteration.md](./33-autonomous-iteration.md)  
- [24-content-operations.md](./24-content-operations.md)  
- [17-ceo-cockpit.md](./17-ceo-cockpit.md)  
- [runbooks/product-intel-lounge.md](../runbooks/product-intel-lounge.md) — 上线检查清单  
