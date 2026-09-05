# 08 — 多项目 / 多站点组合管理

## 公司 vs 项目

| 层级 | 载体 | 内容 |
|------|------|------|
| **公司** | `.ai-company/`（可独立 repo：`company-harness`） | 宪法、模板、默认 merge-policy |
| **项目** | 各产品 git repo | `.delivery/<slug>/`、项目 CLAUDE.md |
| **任务** | GitHub Issue / Multica Issue | brief、AC、labels |

**一个 CEO、多个产品线** = 一个 Multica workspace（或多 workspace 按业务线分）+ 多个 repo。

**业务线类型：**

| `kind` | 执行面 | HQ 本机 path |
|--------|--------|----------------|
| `product`（默认） | cursor-agent / GHA | 需要 `AI_REPO_PATH_*`（`--local`） |
| `content` | 远程 Hermes | **不需要**；见 [24-content-operations.md](./24-content-operations.md) |

---

## 项目台账

维护 [templates/project-registry.yaml](../templates/project-registry.yaml)：

```yaml
projects:
  - id: music-game-sea
    repo: github.com/your-org/music-game
    tier: production          # production | staging | experiment
    stack: next-go
    autopilot_id: ap_xxx
    max_nightly_tickets: 2
    e2e: true
    openapi: true
  - id: landing-tool-a
    repo: github.com/your-org/tool-a
    tier: experiment
    max_nightly_tickets: 1
    e2e: false
    openapi: false
```

CEO 每周更新：优先级、`max_nightly_tickets`、是否暂停。

---

## Harness 复制策略

### 方案 A — 每 repo 复制（简单）

```bash
# 从新项目根目录执行
cp -r /path/to/company-harness/.delivery .
cp -r /path/to/company-harness/.cursor/agents .cursor/
cp /path/to/company-harness/.github/workflows/agent-delivery-*.yml .github/workflows/
```

### 方案 B — Git submodule（推荐 ≥3 项目）

```bash
git submodule add https://github.com/your-org/company-harness .harness
# CI 中引用 .harness/scripts/
```

### 方案 C — 组织级 Template Repo

GitHub Template → 新项目 `Use this template` 已含 harness。

---

## 规范同步（Company OS → 产品仓）

执行 harness **不**复制 `.ai-company/docs/` 全文。规范副本走独立管道：

```bash
# 编辑 manifest 后，同步到 registry 内各本机 checkout
bash scripts/ai-company/sync-company-norms.sh
bash scripts/ai-company/sync-company-norms.sh --id beatscape --harness --force-harness
```

产物：各产品 `.delivery/company-os/` + 更新的 `COMPANY-OS.md`。

详见 [27-norm-sync.md](./27-norm-sync.md) 与 [config/company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml)。

SecondBrain 层（`sync-all-harness.sh`）与 harness 层（`install-harness.sh`）见同文档 — **三条管道勿混用**。

---

## 队列调度（多项目公平）

夜间总配额示例：`max_total_tickets: 5`

```text
按 project-registry 的 priority 排序
    → 每个项目不超过 max_nightly_tickets
    → 轮询各 repo 的 agent-safe backlog
    → 达总上限停止
```

实现：CEO 本机 `portfolio-dispatch.sh` 读 registry，或 LangGraph 节点读 registry。

---

## Multica 组织方式

| 模式 | 适用 |
|------|------|
| **单 workspace 多项目** | 小团队，Issue 用 label `project:music-game` |
| **每产品线一 workspace** | 隔离权限与预算 |
| **每 repo GitHub + 统一 Autopilot** | webhook 带 `repo` 字段路由 |

Autopilot runbook 示例：

```bash
multica autopilot create \
  --title "Nightly portfolio sweep" \
  --description "Read project-registry. Process agent-safe issues per project caps." \
  --agent <dev-agent> \
  --mode create_issue

multica autopilot trigger-add <id> --kind schedule \
  --cron "0 2 * * *" --timezone Asia/Shanghai
```

---

## 实验站 vs 生产站

| 类型 | merge-policy | E2E | CEO merge |
|------|--------------|-----|-----------|
| experiment | 宽 allow | 可选 | 可 auto-merge docs/fix |
| production | 严 deny | 必须 | 敏感路径人工 |

---

## 新项目 SLA（内部）

| 里程碑 | 时间盒 |
|--------|--------|
| harness 接入完成 | 1 天 |
| 首个 agent-safe ticket 跑通 | 3 天 |
| 纳入夜间 cron | 1 周内 |

详见 [runbooks/onboard-new-project.md](../runbooks/onboard-new-project.md)。

---

## 相关文档

- [templates/project-registry.yaml](../templates/project-registry.yaml)  
- [10-cost-and-budget.md](./10-cost-and-budget.md)  
