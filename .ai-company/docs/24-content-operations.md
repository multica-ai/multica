# 24 — 自媒体运营（Content line）

> 远程 Hermes 机执行 · CEO 本机指挥 · GitHub Issue 为队列真相源  
> 更新：2026-08-29

## 定位

| 层 | 载体 | 自媒体 |
|----|------|--------|
| 经营 | SecondBrain OPC | 本周是否起号、杀线 |
| 指挥 | `.ai-company/` + `:9477` + `ceo-nightly` | portfolio 派单、飞书 BLOCKED |
| 队列 | **GitHub Issues（内容仓）** | `agent-safe` 草稿任务 |
| 执行 | **远程 Hermes 机** | `dispatch-hermes-cli.sh` |
| 发布 | **CEO human-only** | 除非 Issue 显式 `publish-ok` |

Hermes **只做 Worker**，不顶层调度。流程决策在 `portfolio-dispatch.sh` / `pull-dispatch.sh`。

**硬规则：** 内容线与工程线共用 HQ，但 **队列真相源只有一个** — GitHub Issues。远程 Hermes Kanban、飞书私聊、Multica Inbox 均不得与 Issues 并行派单。

---

## 职责划分（Harness 权威）

> **司令部 vs 内容工位** — 完整表见 harness 源文件  
> [content-hq-split.md](../harness/content-hq-split.md)（安装内容仓时复制为 `.delivery/CONTENT-HQ-SPLIT.md`）

| 机器 | 角色 |
|------|------|
| **CEO 本机（multica HQ）** | 定队列、开关业务线、工程 cursor 交付、`ceo-nightly`、飞书 BLOCKED、发布拍板 |
| **远程 Hermes（lighthouse）** | 只执行内容仓 Issue：`pull-dispatch` → Hermes oneshot → `drafts/` + PR |

### 本机做 / 不做

| ✅ 本机 | ❌ 本机不做 |
|--------|------------|
| `kind: product` 工程派单（cursor） | `cursor-agent` 写自媒体长稿 |
| `project-registry` · `paused` / `priority` | `AI_REPO_PATH_content_*` |
| 内容线：Issues 建票 + `portfolio-dispatch` 触发（gha / 仅统计） | 远程 Hermes / Kanban 派单 |
| 21:00 飞书日报、审批桥 | 在远程跑 `ceo-nightly` |
| 发帖、投流、账号绑定（human-only） | |

### 远程做 / 不做

| ✅ 远程 | ❌ 远程不做 |
|--------|------------|
| `kind: content`：`dispatch-hermes-cli.sh` | 公司级编排、自创顶层 cron 派单 |
| profile `zimeiti` + 22:00 `pull-dispatch --max-tasks 1` | 工程仓 cursor / 全仓 CI |
| 内容仓 branch + PR | Kanban 与 Issues **双派单** |
| `gh auth`（内容仓 **必须**） | `ceo-nightly`、飞书审批桥 |

### 日常对照（速查）

| 事项 | 本机 | 远程 |
|------|:----:|:----:|
| 写 Issue | ✅ | |
| Hermes 写稿 / PR | | ✅ |
| 工程交付 | ✅ | |
| 21:00 日报 | ✅ | |
| 22:00 内容执行 | | ✅ |
| 平台发布 | ✅ 人点 | |

**按仓分** `product` → 本机 · `content` → 远程。**按 token 分**：两边默认 `max_nightly_tickets: 1` / `--max-tasks 1`。

---

## 双工作台（一入口、两权威 UI）

CEO **只书签一个入口**：本机 `http://127.0.0.1:9477`。工程状态在指挥舱内看；内容 pack 审稿 **新标签打开** `hq.revoices.app`（`:9477` 内已固定按钮，不 iframe）。

| 拉界面 | URL | 机器 |
|--------|-----|------|
| 工程 + 公司总览 | `http://127.0.0.1:9477` | CEO 本机 |
| 内容审稿 / pack | `https://hq.revoices.app/#content/review` | lighthouse（公网） |
| Worker（可选） | `https://agent.revoices.app/` | lighthouse tunnel |

registry 可选字段：`content_workbench_url`（每内容线覆盖默认 HQ URL）。

---

## 远程 Hermes 机（生产环境）

当前自媒体执行机：**腾讯云 Lighthouse**（OpenCloudOS）。

| 项 | 值 |
|----|-----|
| SSH（CEO 本机） | `ssh lighthouse`（`~/.ssh/config` → `root@43.153.104.71`，密钥 `tencent_lighthouse`） |
| Hermes | v0.20.4，`/root/.local/bin/hermes` |
| 自媒体 profile | `zimeiti`（`~/.hermes/profiles/zimeiti/`） |
| 默认模型 | `deepseek-v4-flash`（profile 经 `b-ai` 聚合，非必直连 DeepSeek API） |
| 遗留 HQ | `/root/SOP_AI_COMPANY`（SecondBrain + 公司框架，**文档/记忆保留**） |
| 遗留调度 | Hermes **Kanban**（`~/.hermes/kanban.db`）— **整合后降级，不作主队列** |

### 现状快照（2026-08-29 勘查）

| 检查项 | 状态 |
|--------|------|
| `gh auth` | ❌ 未登录 — multica 内容线 **阻塞项** |
| `content-youtube-sea` 仓 | ❌ GitHub 尚未创建 |
| content harness / `pull-dispatch` | ❌ 未安装 |
| GHA self-hosted runner | ❌ 未注册 |
| 常驻 Hermes gateway | ⚠️ 多 profile 同时运行（`zimeiti`、`sop-game`、`revoices` 等） |
| 资源 | ⚠️ ~2GB 内存、磁盘 ~86% — 不宜再叠长驻 Agent |

---

## 双栈整合（multica HQ ↔ 远程 Hermes）

远程机 **已在跑** 一套自媒体栈（`zimeiti` gateway + Kanban + `/root/SOP_AI_COMPANY`）。multica 内容线要求 **另立 GitHub Issue 队列**。整合目标：

```text
┌─────────────────────────────────────────────────────────────┐
│ CEO 本机 (multica HQ)                                       │
│  project-registry · ceo-nightly · 飞书 BLOCKED · :9477       │
│  portfolio-dispatch（content → gha 或仅统计）                │
└───────────────────────────┬─────────────────────────────────┘
                            │ GitHub Issues（真相源）
                            ▼
┌─────────────────────────────────────────────────────────────┐
│ 远程 lighthouse                                              │
│  content-<channel>/  ·  pull-dispatch.sh  ·  Hermes oneshot  │
│  profile: zimeiti（执行）  ·  Kanban（只读/逐步退役）          │
└─────────────────────────────────────────────────────────────┘
```

### 原则

1. **Issue 派单，Hermes 执行** — HQ 不跑 `cursor-agent` 做内容；远程不自创顶层 cron 编排（对齐 [company-defaults.yaml](../config/company-defaults.yaml)：`hermes` review only，`hermes_self_evolution: false`）。
2. **避免双派单** — Kanban 自动出队与 `pull-dispatch.sh` **二选一**；整合期优先 GitHub Issues。
3. **CEO 本机无路径** — 不配置 `AI_REPO_PATH_content_*`（见 [19-asset-registry.md](./19-asset-registry.md)）。
4. **发布 human-only** — Hermes 写 `drafts/`、开 PR；发帖/投流 CEO 点发送。

### 推荐模式：`remote-pull`（先于此文档中的 `gha`）

| 模式 | 适用 | 说明 |
|------|------|------|
| **`remote-pull`**（推荐起步） | 单机 Hermes、无 runner | HQ 只统计；远程 cron `pull-dispatch.sh --max-tasks 1` |
| `gha` | 已装 self-hosted runner | HQ `portfolio-dispatch` → `gh workflow run` |
| `local` | — | **不支持** content（脚本 skip） |

registry 示例（起号前 `paused: true`）：

```yaml
- id: content-youtube-sea
  kind: content
  repo: github.com/chenzh/content-youtube-sea
  dispatch_mode: remote-pull    # 起步推荐；runner 就绪后改 gha
  workflow: content-delivery-dispatch.yml
  executor: remote-hermes
  max_nightly_tickets: 1
  paused: true
  publish_policy: ceo-approve
  channels: [youtube, x]
```

---

## 落地顺序（整合清单）

按序执行；前一步未完成不要开派单。

### 阶段 0 — 远程前置（lighthouse）

```bash
ssh lighthouse
export PATH="$HOME/.local/bin:$HOME/.bun/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

# 1. GitHub CLI（阻塞项）
gh auth login

# 2. 确认 Hermes 可用
hermes version
# 自媒体执行建议固定 profile：
export HERMES_PROFILE=zimeiti   # 或 dispatch 脚本内 -p zimeiti
```

### 阶段 1 — 内容仓 + harness

**CEO 本机（multica 仓）：**

```bash
# 先在 GitHub 创建 chenzh/content-youtube-sea（空仓即可）
git clone git@github.com:chenzh/content-youtube-sea.git
bash scripts/ai-company/install-content-harness.sh /path/to/content-youtube-sea
cd /path/to/content-youtube-sea && git push origin main
```

**远程机：**

```bash
cd ~
git clone git@github.com:chenzh/content-youtube-sea.git
cd content-youtube-sea
# harness 已在仓内；补 agent-safe / agent-running / agent-blocked / agent-done 标签
```

试跑：

```bash
cd ~/content-youtube-sea
bash scripts/content-delivery/pull-dispatch.sh --max-tasks 1 --dry-run
bash scripts/content-delivery/dispatch-hermes-cli.sh <issue#> --dry-run
```

### 阶段 2 — 远程 cron（`remote-pull`）

比 CEO `ceo-nightly`（21:00）**晚 1 小时**，避免与工程线抢带宽：

```cron
PATH=/root/.local/bin:/root/.bun/bin:/usr/local/bin:/usr/bin:/bin
0 22 * * * cd /root/content-youtube-sea && bash scripts/content-delivery/pull-dispatch.sh --max-tasks 1 >> /root/.multica/content-pull-dispatch.log 2>&1
```

### 阶段 3 — HQ registry + 验收

**CEO 本机：**

1. [project-registry.yaml](../templates/project-registry.yaml) 登记 `kind: content`，`dispatch_mode: remote-pull`
2. 起号时 `paused: false`，`max_nightly_tickets: 1`
3. 验收：

```bash
bash scripts/ai-company/portfolio-dispatch.sh --dry-run --max-total 2
# 应看到 kind=content dispatch=remote-pull，且不尝试 local cursor
bash scripts/ai-company/verify-hands-off.sh
```

### 阶段 4 — 退役 Kanban 主队列（手动）

1. 停止 Kanban **自动派单**（保留 DB 作历史）
2. `/root/SOP_AI_COMPANY` 保留为 SecondBrain / 框架文档，**不再**作为任务真相源
3. 新任务一律进内容仓 GitHub Issues（`content_agent_safe_task.yml` 模板）

### 阶段 5 — 可选 GHA runner

GitHub → 内容仓 → Actions → self-hosted runner，标签：`self-hosted`, `content-hermes`。  
然后将 registry `dispatch_mode` 改为 `gha`，HQ 夜间可 `gh workflow run`。

### 阶段 6 — 可选 Multica runtime 可观测

```bash
export MULTICA_SERVER_URL=https://multica.<tailnet>.ts.net
multica login && multica daemon start
```

仅用于 UI 看 runtime 在线；**Issue 真相源仍为 GitHub**。

---

## DeepSeek / LLM Token 节省

远程机当前最大开销：**多 profile 常驻 gateway** + 高频 cron 唤醒 LLM。整合时一并治理。

### 立刻见效（优先级高）

| 动作 | 原因 |
|------|------|
| **只留 `zimeiti` gateway 常开** | 其余 profile（`sop-game`、`revoices`、`xuqiu`、`openmate`…）按需手动起 |
| 内容任务用 **`hermes … --oneshot`** | `dispatch-hermes-cli.sh` 已如此；避免为每张票开长会话 |
| `pull-dispatch --max-tasks 1` | 每次 cron 最多 1 张 Issue |
| registry `max_nightly_tickets: 1` | HQ 公平队列上限 |

### Hermes 配置（`~/.hermes/config.yaml` / profile）

| 配置 | 建议 |
|------|------|
| `smart_model_routing.enabled` | `true` — 短 prompt 走便宜模型 |
| `show_token_analytics` | `true` — 先看清谁在烧 |
| `hygiene_hard_message_limit` | 保持 ~400，限制上下文膨胀 |
| `fallback_providers` | 收窄；失败重试会重复计费 |
| 自媒体执行 | 统一 `zimeiti` + `b-ai`，避免部分任务直连 `DEEPSEEK_API_KEY` |

### Cron 减负

整合后合并远程 LLM cron：

- `hq_cron_alert` / `hq_w1_guard` 等 **每小时** 脚本 → 改为纯脚本健康检查（无 LLM）或并入 CEO 21:00 飞书日报
- `zbrain-loop` → 与 OPC 节奏对齐，避免与 `pull-dispatch` 同夜并行

### 与 multica 默认对齐

见 [company-defaults.yaml](../config/company-defaults.yaml)：

- `hermes`：**评审 / 执行单票**，不做公司级编排
- `hermes_self_evolution: false` — 无人值守流水线禁止自进化循环

---

## 仓库结构（内容仓）

```text
content-<channel>/
  brand/voice.md
  drafts/YYYY-MM-DD-topic/
  calendar/YYYY-MM.yaml
  .delivery/prompts/orchestrator-kickoff.md
  scripts/content-delivery/
  .github/workflows/content-delivery-dispatch.yml
```

安装（multica HQ 执行，推送到 GitHub 后远程 `git clone`）：

```bash
bash scripts/ai-company/install-content-harness.sh /path/to/content-youtube-sea
```

---

## project-registry 字段

见 [templates/project-registry.yaml](../templates/project-registry.yaml) 注释。

| `dispatch_mode` | CEO 本机行为 | 远程机行为 |
|-----------------|--------------|------------|
| `gha` | `portfolio-dispatch` → `gh workflow run` | self-hosted runner 跑 Hermes |
| **`remote-pull`** | 只统计，不派单 | cron: `pull-dispatch.sh --max-tasks 1` |
| `local` | **不支持** content（会 skip） | — |

**不要**在 CEO 机配置 `AI_REPO_PATH_content_*`。

---

## 任务分级（内容）

在 [06-task-grading.md](./06-task-grading.md) 基础上：

| 分级 | 自媒体示例 |
|------|------------|
| `agent-safe` | 调研摘要、标题库、草稿、日历提案 |
| `agent-assisted` | 成稿、多平台改写 → CEO 审后排期 |
| `human-only` | 发帖、投流、账号绑定、带 `publish-ok` 仍建议人点发送 |

Issue 模板：`content_agent_safe_task.yml`（harness 安装）。

---

## 与网站线共存

- **工程** `kind: product` → `portfolio-dispatch`（本机 CLI）+ `agent-delivery-gate.yml`
- **内容** `kind: content` → `content-delivery-dispatch.yml` 或 **`remote-pull`**
- OPC 每周只开一条全力主线，用 `paused` + `priority` 控带宽

---

## 脚本索引

| 脚本 | 位置 |
|------|------|
| `install-content-harness.sh` | `scripts/ai-company/` |
| `dispatch-hermes-cli.sh` | `scripts/content-delivery/` |
| `pull-dispatch.sh` | 同上（远程 cron） |
| `content-verify.sh` | 轻量 merge 前检查 |
| `portfolio-dispatch.sh` | 读 registry `kind` / `dispatch_mode` |

---

## 故障排查

| 现象 | 处理 |
|------|------|
| `gh: not logged in`（远程） | `ssh lighthouse` → `gh auth login` |
| HQ 派单 content 被 skip | 检查 `kind: content`；勿设 `AI_REPO_PATH_*` |
| 双份任务（Kanban + Issue） | 停 Kanban 自动派单，只保留 Issues |
| 内存爆 / swap 高 | 减常驻 gateway；`--max-tasks 1`；清磁盘（`disk_guard.sh` 已有） |
| Token 异常飙升 | 开 `show_token_analytics`；查 `~/.hermes/logs/agent.log` |

---

## 相关

- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md)
- [13-opc-bridge.md](./13-opc-bridge.md)
- [15-feishu-site-factory.md](./15-feishu-site-factory.md)（网站线对称能力）
- [19-asset-registry.md](./19-asset-registry.md)
- [23-local-agent-environment.md](./23-local-agent-environment.md)（仅工程本机）
