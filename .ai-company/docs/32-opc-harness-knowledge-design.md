# 32 — OPC Harness 与知识库设计方案

> **层级**：HQ 战略 / 架构（不下发 `company-os`）  
> **状态**：active · **更新**：2026-08-29  
> 配套：[28-norm-layers.md](./28-norm-layers.md) · [27-norm-sync.md](./27-norm-sync.md) · [31-harness-learnings-routing.md](./31-harness-learnings-routing.md) · [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) · [13-implementation-roadmap.md](./13-implementation-roadmap.md)

---

## 1. 设计目标

在 **一人公司（OPC）+ 多产品 portfolio + Agent 派单** 前提下，同时满足：

| 目标 | 含义 |
|------|------|
| **全对话遵循 harness** | 每条 Cursor 会话知道读什么、冲突怎么解 |
| **省 token** | 常驻上下文只有薄指针，宪法全文按需 `Read` |
| **单一真相源（SSOT）** | 同一规则只写一处，别处链接（硅谷纪律） |
| **经验可回流** | 里程碑/BLOCKED 经验能升格到正确规范层，不靠聊天记忆 |
| **可验收** | 脚本 `verify-*` 证明接线没断 |

**非目标（刻意不做）：**

- 把 Vault / company-os 全文塞进 Cursor Rules 或 User Rules  
- Agent 无审批自动 PATCH 宪法正文  
- 用飞书/聊天当规范真相源  

---

## 2. 知识库承载（联邦架构）

```text
┌─────────────────────────────────────────────────────────────┐
│ 对话（Cursor）— 默认丢弃；只留摘要 / worklog / deposit       │
└───────────────────────────┬─────────────────────────────────┘
                            │ 沉淀（AUTO-DEPOSIT / 31 路由）
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
 SecondBrain Vault     multica/.ai-company/    产品仓
 (Git + Markdown)      Company OS 权威         .delivery/<slug>/
 HARNESS/ · INBOX/     docs/ · runbooks/       brief · accept_cases
 Wiki 永久页           → sync company-os/      GitHub Issue（单票）
```

| 层 | 回答什么 | 权威载体 | Agent 怎么读到 |
|----|----------|----------|----------------|
| **0 OPC / Vault** | 值不值得做、编码习惯、会话协议 | `SecondBrain/10-SYSTEM/HARNESS/` | `vault-harness.mdc` → `docs/VAULT-HARNESS.md` |
| **1 Company OS** | 工厂怎么转、DoD、BLOCKED | `.ai-company/docs/` → `.delivery/company-os/` | `company-harness.mdc` |
| **2 项目** | 这站做什么、怎么验 | `CLAUDE.md` + `.delivery/<slug>/` | `CLAUDE.md` + kickoff |
| **3 任务** | 这票范围 | GitHub Issue | Issue body |

**冲突优先级**（越后越具体）：任务 > 项目 brief > 项目 CLAUDE > company-os > Vault global。

**两套 harness 不混 todo**：Multica 产品开发走 SecondBrain + `CLAUDE.md`；卫星交付走 `.ai-company/harness` + `.delivery/`。

---

## 3. 对话层：Tier-0 薄指针（省 token）

### 3.1 注入策略

| Tier | 机制 | 体量 | 内容 |
|------|------|------|------|
| **0** | `.cursor/rules/*.mdc` `alwaysApply: true` | ~600–800 token/对话 | 阅读顺序指针 only |
| **1** | Agent 开干前 `Read` | 按需 | `VAULT-HARNESS.md`、`company-os/`、`CLAUDE.md` |
| **2** | Issue / kickoff | 单票 | AC、out of scope |

### 3.2 各仓 Tier-0 规则集

| 仓类型 | 规则文件 |
|--------|----------|
| **multica HQ** | `vault-harness` · `zbrain-session` · `company-harness` · `code-index` |
| **产品仓（有 `.delivery`）** | `company-harness`（必须）；有 `.secondbrain` 时再加 `vault-harness` · `zbrain-session` |
| **仅 SecondBrain 外接** | `vault-harness` · `zbrain-session` |

**User Rules**：只放个人偏好（语言、commit 习惯），**禁止**重复 harness 正文。

### 3.3 同步与安装（三条管道）

| 管道 | 命令 | 落地 |
|------|------|------|
| Vault harness | `sync-all-harness.sh` | `vault-harness.mdc` + `VAULT-HARNESS.md` |
| company-harness 执行件 | `install-harness.sh` | agents、workflows、`company-harness.mdc` |
| Company OS 宪法副本 | `sync-company-norms.sh` | `.delivery/company-os/` |

**本机一键**：`rollout-harness-tier0.sh` · 验收：`verify-harness-tier0.sh`

---

## 4. 经验回流闭环（写回 harness）

### 4.1 问题

Tier-0 解决了「读」；「写」若只靠人工改 Markdown，规范进化慢于 BLOCKED 重复发生。

### 4.2 方案（已实现 2026-08-29）

```text
里程碑 / BLOCKED 解决 / 口令「记录 harness 经验」
    → record-harness-learning.sh（关键词路由 + 密钥拒绝）
    → harness-candidates.md（待审队列）
    → CEO 周回顾升格 ≤3 条
    → PATCH 目标 doc（06/18/21/brief/Vault HARNESS…）
    → sync-company-norms / sync-all-harness
```

| 组件 | 路径 |
|------|------|
| 路由表 | [31-harness-learnings-routing.md](./31-harness-learnings-routing.md) |
| 候选队列 | [system-evolution/harness-candidates.md](./system-evolution/harness-candidates.md) |
| 记录 | `scripts/ai-company/record-harness-learning.sh` |
| 验收 | `scripts/ai-company/verify-harness-learnings.sh` |

**硬约束**：禁止无审批 PATCH `.cursor/rules` 或把经验 append 进 `alwaysApply` 规则。

**SecondBrain 侧**：仍走 `AUTO-DEPOSIT` Rule A/C + Rule W Wiki Ingest；31 负责 **Company OS / 项目层** 升格路由。

---

## 5. 与硅谷标准对照

依据 [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) 与 [13-implementation-roadmap.md](./13-implementation-roadmap.md)。

### 5.1 已对齐

- SSOT + manifest 下发 + fork 纪律  
- Issue = 可执行 Spec（AC 用命令）  
- Sleep Mode + Verifier 认 exit code  
- Runbook 可执行 + `verify-hands-off.sh`  
- Harness 可复制 + Tier-0 省 token  
- 指挥舱分工（飞书推 / `:9477` 拉）  

### 5.2 缺口与优先级

| 优先级 | 缺口 | 硅谷为何在意 | 下一步 | 状态（2026-08-29） |
|--------|------|--------------|--------|-------------------|
| **P0** | 周指标自动化（派/绿/堵、$/PR） | 无数据无法进化 | `ceo-weekly-metrics.sh` + dashboard JSON | **已接线** — `$/PR` 仍手填（见 `10-cost-and-budget`） |
| **P0** | 本机派单真并行或 UI 诚实标注 | 容量规划不能靠感觉 | `PORTFOLIO_DISPATCH_ASYNC` + workbench `local-cli-async` pill | **已接线** — 默认 async nohup；真并行仍受 cursor-agent 上限 |
| **P1** | 活跃项目 harness + company-os 全绿 | 副本漂移 | `rollout-harness-tier0` + `sync-company-norms` | **运营项** — `verify-harness-tier0` 扫 portfolio |
| **P1** | ≥3 站各 merge 1 trivial PR | P1 工厂可复制证明 | 取消主力线 `paused` + onboard | **运营项** — 非脚本缺口 |
| **P1** | harness-candidates 周升格制度化 | 知识闭环 | `ceo-weekly-harness-review.sh` 每周一 | **已接线** — CEO 仍须 PATCH ≤3 条 |
| **P2** | 成本自动 pause + 用量拉取 | FinOps | `10-cost-and-budget` 接真实账单 | **暂缓** |
| **P2** | 出海站合规 CI 强制 | 上线风险 | `compliance-checklist` 模板进 CI | **暂缓** |
| **P3** | `company-harness` 独立 repo | 规模化 | 路线图 P3 | **暂缓** |
| **P3** | LangGraph 硬编排 | ticket >10/天 | 路线图 P2 | **暂缓** |

### 5.3 阶段定位

```text
文档/架构思维  ≈ 硅谷 P1 设计完成
运营成熟度    ≈ P0 → P1 过渡（指标、并发、全 portfolio 一致执行）
```

**P1 成功画面**（摘自 13）：3～5 站同时跑；每晚消化 agent-safe；早上 15 分钟只处理 BLOCKED；不读代码；CI 不绿的不 merge。

---

## 6. OPC 与 Company OS 分工

| 层 | 系统 | CEO 碰什么 |
|----|------|------------|
| **经营** | SecondBrain OPC map、杀线、触达 | 本周主线、砍线 |
| **交付** | Company OS + Issues + 夜间队列 | BLOCKED、AC、投 brief |
| **控制台** | `:9477` 指挥舱 + 飞书 | 推/拉分离 |

**原则**：OPC 决定「做什么值得做」；Company OS 决定「怎么无人值守做完」。Agent 会话不是经营真相源。

---

## 7. 验收清单（设计是否接线）

```bash
# 一键（Tier-0 + learnings + metrics；hands-off 可选）
bash scripts/ai-company/verify-opc-design.sh
bash scripts/ai-company/verify-opc-design.sh --skip-hands-off   # 无 cron/飞书时

# 分项
bash scripts/ai-company/verify-harness-tier0.sh
bash scripts/ai-company/verify-harness-learnings.sh
bash scripts/ai-company/ceo-weekly-metrics.sh --json
bash scripts/ai-company/verify-hands-off.sh

# 周一 ritual（指标写周报 + 候选队列 + verify）
bash scripts/ai-company/ceo-weekly-harness-review.sh
```

| 检查项 | 通过标准 |
|--------|----------|
| multica HQ 4 条 Tier-0 | verify-harness-tier0 绿 |
| 产品仓 company-harness | install-harness 已跑 |
| company-os 副本 | sync-company-norms 后 commit |
| 候选队列可写 | record-harness-learning 试跑 |
| 周指标 派/绿/堵 | ceo-weekly-metrics.sh --json 有 blocked/running/queue/merged |
| 派单模式标注 | workbench `/api/meta` → `local-cli-async`（默认 async） |
| 周回顾模板 | system-evolution README 含 harness 升格节 |

---

## 8. 相关文档与脚本

| 类型 | 链接 |
|------|------|
| 分层 | [28-norm-layers.md](./28-norm-layers.md) |
| 同步 | [27-norm-sync.md](./27-norm-sync.md) |
| 布局 | [29-harness-layout.md](./29-harness-layout.md) |
| 经验路由 | [31-harness-learnings-routing.md](./31-harness-learnings-routing.md) |
| 硅谷纪律 | [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) |
| 路线图 | [13-implementation-roadmap.md](./13-implementation-roadmap.md) |
| OPC 桥接 | [13-opc-bridge.md](./13-opc-bridge.md) |
| Harness 索引 | [../harness/HARNESS-INDEX.md](../harness/HARNESS-INDEX.md) |
| 系统进化 | [system-evolution/README.md](./system-evolution/README.md) |
| 周指标 | `scripts/ai-company/ceo-weekly-metrics.sh` |
| 周一回顾 | `scripts/ai-company/ceo-weekly-harness-review.sh` |
| 设计验收 | `scripts/ai-company/verify-opc-design.sh` |

---

## 变更记录

| 日期 | 变更 |
|------|------|
| 2026-08-29 | 初版：联邦知识库 + Tier-0 + 经验回流 + 硅谷差距与 P0–P3 优先级 |
| 2026-08-29 | 补 P0/P1 脚本接线：ceo-weekly-metrics、verify-opc-design、ceo-weekly-harness-review；派单 local-cli-async 标注 |
