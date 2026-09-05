# Agent 自主交付流水线（Sleep Mode）

让 Agent 团队按 ticket 自主实现、验证、开 PR；你只投队列、看告警、对验收用例点勾。**CI 不绿不算交付。**

本目录是这套流水线的**唯一业务真相源**（对话不是）。架构约束仍以仓库根目录 `CLAUDE.md` / `AGENTS.md` 为准。

---

## 架构一览

```text
┌─────────────────────────────────────────────────────────────────┐
│  你（5 分钟/ ticket）                                            │
│  创建 GitHub Issue（agent-safe 模板）或 .delivery/<slug>/ 文件   │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  调度层（CEO 本机 — 硬逻辑，不靠 Prompt 自觉）                    │
│  · ceo-nightly.sh（21:00 cron）→ portfolio-dispatch（local CLI） │
│  · 或手动：dispatch-cursor-agent-cli.sh / 工作台「智能派单」        │
│  · 或 Multica Autopilot（dogfood，见下文「路径 C」）              │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  执行层：本机 cursor-agent（一 ticket 一 worktree 一 branch）     │
│  读 CLAUDE.md + .delivery/* + Issue 正文                         │
│  子角色：.cursor/agents/{planner,implementer,verifier,reviewer}  │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  门禁层（只信 exit code）                                         │
│  1. Verifier 跑 make check / 范围更窄的 pnpm test + make test     │
│     · 复刻/落地页另跑 make visual-check（Playwright @visual）     │
│     · 需 competitor_inventory.md + wont_do.md（见 Visual Replica）│
│  2. GitHub CI（现有 ci.yml / cloudflare-pages-check）             │
│  3. agent-delivery-gate：路径白名单 → 可选 auto-merge            │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  你睡觉时：Slack/邮件 仅在 BLOCKED / CI 挂 / 需澄清 时 ping       │
└─────────────────────────────────────────────────────────────────┘
```

---

## 三种落地路径

| 路径 | 适合 | 你需要做什么 |
|------|------|--------------|
| **A. 手动 CLI** | 试跑、单票 | `dispatch-cursor-agent-cli.sh <issue#>` |
| **B. 夜间 portfolio（推荐睡觉模式）** | 多仓公平队列 | `ceo-nightly.sh` + `cursor-agent login` |
| **C. Multica Autopilot（dogfood）** | 用自家产品管队列 | `multica autopilot create` + cron |

路径 B 在 **CEO 本机** 跑；GitHub Actions 只做 **CI / gate**，不调用 Cursor Cloud API。

---

## 第 0 步：前置条件

### Cursor（CEO 本机）

- Cursor **Pro / Business**
- 本机已登录：`cursor-agent login`（`cursor-agent status` 显示 Logged in）
- **不支持** `CURSOR_API_KEY` / Cloud Agents API 派单路径

### GitHub 仓库

1. **Settings → Secrets → Actions**（可选）：

   | Secret | 用途 |
   |--------|------|
   | `SLACK_WEBHOOK_URL` | BLOCKED / gate 失败告警 |

2. **Labels**（Settings → Labels）创建：

   | Label | 含义 |
   |-------|------|
   | `agent-safe` | 允许进夜间队列 |
   | `agent-running` | 已有 Agent 在处理 |
   | `agent-blocked` | NEED_CLARIFY 或 3 轮 verify 失败 |
   | `agent-done` | PR 已开且 CI 绿（人工或 bot 打） |

3. **Branch protection（main）**  
   保持现有 required checks（`frontend` / `backend`）。auto-merge **仅**对白名单路径开启（见 `config/merge-policy.json`）。

---

## 第 1 步：任务分级（决定能不能睡觉）

### ✅ 允许 `agent-safe`

- 有**可勾选验收标准**（Issue 模板或 `accept_cases.md`）
- 改动范围 ≤ 3 个模块，**无** DB migration
- 不碰 auth/支付/权限模型
- 有现成测试模式可抄

### ❌ 禁止进队列

- 新 API 语义 / Breaking change
- `server/migrations/**` 变更
- 「用户可能会喜欢…」类产品判断
- 跨 5+ 包的 refactor

违反分级 = 醒来收尸，不是 Agent 的锅。

---

## 第 2 步：创建任务

### 方式 1：GitHub Issue（推荐）

使用模板 **Agent-safe task**，填：

- What & why（3 句以内）
- Acceptance criteria（`- [ ]` 列表）
- Out of scope（禁止改的路径）

打 label **`agent-safe`**。

### 方式 2：本地 delivery 包

```bash
cp -r .delivery/_template .delivery/my-feature
# 编辑 brief.md、accept_cases.md
```

大功能先在 `.delivery/<slug>/plan.md` 里过一遍（Planner subagent 也会写这个）。

---

## 第 3 步：Subagent 角色（已配置）

文件在 `.cursor/agents/`：

| 文件 | 角色 |
|------|------|
| `orchestrator.md` | 总调度，禁止跳阶段 |
| `planner.md` | 出 plan + 补全 accept_cases |
| `implementer.md` | 写代码 + 单测 |
| `verifier.md` | **只跑测试**，输出 PASSED/BLOCKED |
| `reviewer.md` | 对照 CLAUDE.md 审查 |

Cloud Agent clone 仓库后会自动读取项目 subagent 定义（手动会话时）。

---

## 第 4 步：路径 A — 手动试跑（今天就能用）

```bash
# 单票（在 CEO 本机、已 checkout 的产品仓或设 REPO_ROOT）
bash scripts/agent-delivery/dispatch-cursor-agent-cli.sh <issue_number>
```

或粘贴 `.delivery/prompts/orchestrator-kickoff.md` 到 Cursor Agent 会话（手动模式）。

---

## 第 5 步：路径 B — 夜间自动调度（CEO 本机）

```bash
# 21:00 cron（推荐）
bash scripts/ai-company/install-nightly-cron.sh --install

# 或手动
bash scripts/ai-company/portfolio-dispatch.sh --max-total 3
bash scripts/ai-company/ceo-nightly.sh
```

脚本会：

1. 按 `project-registry.yaml` 公平扫描各仓 `agent-safe` 队列
2. 对每个 issue 调 `dispatch-cursor-agent-cli.sh`
3. 打 label `agent-running`；失败 → `agent-blocked` + 飞书/Slack（若配置）

**不在 GitHub-hosted runner 上派单** — 机器须 21:00 常开且 `cursor-agent` 已登录。

PR **base 必须是 `main`**，head 来自 `cursor/**` 分支且 CI 全绿时，`agent-delivery-gate.yml` 读取 `config/merge-policy.json`：

- **allow**：docs、纯测试、小范围 fix → squash merge 进 `main`，删 feature 分支
- **deny**：`server/migrations/**`、`packages/core/api/**` 等 → 只开 PR，人早上 merge

合并完成后，本地主仓库应 **`git checkout main && git pull`**（Cloud 路径由 GitHub 完成；本地 CLI 路径用 `finalize-to-main.sh`）。

```bash
# 本地 cursor-agent / worktree 跑完后：
bash scripts/agent-delivery/finalize-to-main.sh --issue <N>
# 或自动：AUTO_FINALIZE_MAIN=1 bash scripts/agent-delivery/dispatch-cursor-agent-cli.sh <N>
```

---

## 第 6 步：路径 C — Multica Autopilot（dogfood）

在你自己的 Multica workspace 里：

```bash
multica autopilot create \
  --title "Nightly agent-safe backlog" \
  --description "Process GitHub issues labeled agent-safe. Read CLAUDE.md and .delivery/README.md. Follow verifier gate." \
  --agent <your-dev-agent> \
  --mode create_issue

multica autopilot trigger-add <id> --kind schedule --cron "0 2 * * *" --timezone Asia/Shanghai
```

Autopilot 创建 issue → 你的 runtime Agent 执行 → 结果写在 issue comment。  
与路径 B 配合：Autopilot 负责**队列与可追溯 run**，GitHub Actions 负责 **API dispatch + merge 策略**。

---

## 第 7 步：告警（真睡觉的关键）

仅以下情况应叫醒你：

| 事件 | 动作 |
|------|------|
| `agent-blocked` | Slack：需澄清或 verify 3 轮失败 |
| CI failed on agent PR | Slack：附 PR 链接 |
| auto-merge 被拒绝（路径 deny） | **不告警**（预期行为，早上处理） |

未配置 `SLACK_WEBHOOK_URL` 时，workflow 只写 GitHub issue comment。

---

## 第 8 步：Orchestrator 硬规则（给 Cloud Agent 的 prompt 摘要）

完整版：`.delivery/prompts/orchestrator-kickoff.md`

核心：

1. 顺序：Planner → Implementer → Verifier → Reviewer → PR  
2. Verifier 必须跑 `make check`（或大改动前的分步 test）；exit ≠ 0 → BLOCKED，最多 3 轮  
3. 歧义 → 输出 `NEED_CLARIFY`，停止，不打 `agent-done`  
4. 禁止改 `CLAUDE.md` 未授权的包边界  
5. 禁止口头「应该没问题」

---

## 验证清单（搭建完成自检）

- [ ] `.cursor/agents/*.md` 已提交
- [ ] GitHub label 四个已创建
- [ ] CEO 本机 `cursor-agent login` 成功
- [ ] 手动 `dispatch-cursor-agent-cli.sh` 或 `portfolio-dispatch` 跑通 trivial ticket
- [ ] Issue → PR → CI 绿
- [ ] `agent-delivery-gate` 对 docs-only PR 行为符合 `merge-policy.json`
- [ ] Slack 测试消息收到（若启用）

---

## 常见问题

**Q: Agent 跳过测试怎么办？**  
A: 不靠 Prompt。Verifier subagent + CI required checks；dispatch script 在 prompt 里写死「PR 描述必须贴 make check 输出」。

**Q: 能否 100% 脱手？**  
A: 不能。你仍负责分级、AC 质量、BLOCKED 处理。睡觉 = 无 BLOCKED 时不叫醒。

**Q: LangGraph 还要吗？**  
A: 队列 <10 ticket/天 不需要。CI + Actions 已是硬 DAG。

**Q: 和 Vibe-coding 单会话的区别？**  
A: 单会话 = 上下文漂移 + 你逐行改。本方案 = 文件真相源 + 子 Agent 隔离 + exit code 门禁。

---

## 目录结构

```text
.delivery/
  README.md                 ← 本文件
  _template/                ← 复制新建 feature
  config/merge-policy.json  ← auto-merge 白名单
  prompts/                  ← 粘贴即用 prompt
.cursor/agents/             ← Subagent 定义
.github/workflows/
  agent-delivery-gate.yml
.github/ISSUE_TEMPLATE/
  agent_safe_task.yml
scripts/agent-delivery/     ← dispatch-cli / finalize / merge check
```
