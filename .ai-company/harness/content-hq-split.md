# Content line — HQ vs remote machine

> **Harness 权威分工表** · 安装到内容仓：`.delivery/CONTENT-HQ-SPLIT.md`  
> 公司级说明：[docs/24-content-operations.md](../docs/24-content-operations.md)

## One-liner

| Machine | Role |
|---------|------|
| **CEO 本机（multica HQ）** | 司令部 + **工程拉界面** `:9477` |
| **远程 Hermes 机（lighthouse）** | 内容工位 + **内容拉界面** `hq.revoices.app` |

**按仓分：** `kind: product` → 本机 cursor · `kind: content` → 远程 Hermes  
**按真相源分：** GitHub Issue 标签为准；Kanban / 飞书 / Multica Inbox 不得并行派单。

---

## CEO 本机（multica HQ）— 做什么

| 类别 | 事项 |
|------|------|
| 战略 | OPC / SecondBrain：起号、杀线、`paused` / `priority` |
| 队列 | 内容任务写在 **GitHub Issues**（`agent-safe` 等）；Work-Finder 补票 |
| 夜间 | `ceo-nightly`（21:00）：工程 reconcile / merge / **local cursor 派单** / 飞书日报 |
| 内容派单 | **登记 + 统计 + 触发**（`remote-pull` 或 `gha`）；**不跑 Hermes** |
| 工程线 | 各 `kind: product` 仓 — `portfolio-dispatch --local` |
| 可见性 | `:9477` 指挥舱（工程 + **链到内容 HQ**）、飞书 | `https://hq.revoices.app/#content/review` 审稿 pack |
| 飞书 | 日报、BLOCKED 审批；**发布前人工**（发帖、投流、绑号） |
| Git | multica 只 push `fork`；产品/内容仓 push `origin` |

### 本机刻意不做

- 不配置 `AI_REPO_PATH_content_*`（内容仓不必在本机 checkout）
- 不用 `cursor-agent` 写自媒体长稿（与工程抢并发、烧本机 token）
- 不把 Hermes 当公司级调度器

---

## 远程 Hermes 机（lighthouse）— 做什么

| 类别 | 事项 |
|------|------|
| 内容执行 | `pull-dispatch.sh` → `dispatch-hermes-cli.sh`（`hermes --oneshot`） |
| 产出 | `drafts/`、`calendar/`、branch `content/issue-<N>`、**PR** |
| 环境 | profile `zimeiti`（建议）；`gh auth` **必须** |
| 定时 | cron 22:00 `pull-dispatch --max-tasks 1`（晚于 HQ 21:00） |
| 重活 | 7×24 频道监听、多媒体流水线 | **内容审稿 UI**（hq.revoices.app，非 multica 指挥舱副本） |
| 遗留 | `/root/SOP_AI_COMPANY`、Kanban DB — **只读/退役**，不作主队列 |

### 远程刻意不做

- 不决定「今晚派哪条线、几张票」（只读 GitHub Issues 执行）
- 不以 Hermes Kanban 与 Issues **双派单**
- 不跑工程产品 `cursor-agent` / 全仓 `make check`（除非 Issue 明确属内容仓）
- 不跑 `ceo-nightly`、飞书审批桥、multica HQ 脚本

---

## 日常对照表

| 事项 | 本机 HQ | 远程 Hermes |
|------|:-------:|:-------------:|
| 写 Issue / backlog | ✅ | |
| Hermes 写稿、开 PR | | ✅ |
| 工程 bugfix / 站点 | ✅ | |
| 21:00 飞书日报 | ✅ | |
| 22:00 内容拉票 | | ✅ |
| 工程仓 auto-merge | ✅ | |
| 内容 PR 审阅 merge | ✅（CEO 或 HQ） | 可开 PR |
| **发帖 / 上传平台** | ✅ **人点** | |
| `gh` 访问内容仓 | 可选 | **必须** |

---

## Token / 资源边界

| 规则 | 说明 |
|------|------|
| 本机 cursor | 仅 `kind: product`；受 `AUTOPILOT_MAX_CONCURRENT` 约束 |
| 远程 Hermes | 仅 `kind: content`；`max_nightly_tickets: 1`，`--max-tasks 1` |
| Gateway | 远程只常开 `zimeiti`；其它 profile 按需手动 |
| 发布 | `publish-ok` 仍建议 CEO 人工点发送 |

---

## 边界原则（Harness 硬约束）

1. **Issue 派单，Hermes 执行** — 对齐 `company-defaults.yaml`：`hermes` worker/review，`hermes_self_evolution: false`。
2. **单一队列** — 整合后 Kanban 自动出队关闭；新任务只进 GitHub Issues。
3. **无 CEO 本机路径** — `resolve-repo-path.sh` 对 `kind: content` 不要求本机 checkout。
4. **发布 human-only** — 除非 Issue 显式 `publish-ok`，且仍建议 CEO 最终点击。

---

## 相关文件

| 文件 | 位置 |
|------|------|
| 本文（内容仓副本） | `.delivery/CONTENT-HQ-SPLIT.md` |
| 运营与接线 | multica `.ai-company/docs/24-content-operations.md` |
| Worker prompt | `.delivery/prompts/orchestrator-kickoff.md` |
| Registry | multica `.ai-company/templates/project-registry.yaml` |
