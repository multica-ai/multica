# AI 公司操作系统（Company OS）

> 权威布局：[docs/29-harness-layout.md](./docs/29-harness-layout.md) · Harness 入口：[harness/HARNESS-INDEX.md](./harness/HARNESS-INDEX.md)

> **你是老板（CEO），这套文档是公司的「宪法 + 运营手册 + 技术蓝图」。**  
> 对话不是真相源；本目录 + 各项目仓库里的 `brief.md` / `accept_cases.md` / `api_spec.yaml` 才是。

本方案面向：**多个网站、多个产品线、长期自主交付**——不是单站一次性脚本。  
与仓库内 [`.delivery/`](../.delivery/README.md) 的关系：`.delivery/` 是 Multica 本仓库的**执行实例**；`.ai-company/` 是**可复用到任意项目**的公司级产品文档。

---

## 一句话定位

```text
CEO 只投方向与验收 → Multica 管任务公司 → LangGraph/GHA 硬编排 → Cursor 等 Worker 只干活
→ CI 硬门禁（lint/test/OpenAPI/E2E）→ 绿才存在 → 你只看仪表盘与 BLOCKED
```

---

## 文档地图

### 产品与战略

| 文档 | 说明 |
|------|------|
| [docs/00-getting-started.md](./docs/00-getting-started.md) | **第一周行动清单**（从这里开始） |
| [docs/01-vision.md](./docs/01-vision.md) | 愿景、「躺平」定义、CEO 保留职能 |
| [docs/02-operating-model.md](./docs/02-operating-model.md) | 公司运转模型：队列、睡眠模式、告警 |
| [docs/03-organization.md](./docs/03-organization.md) | 虚拟编制：角色、权限、谁不能做调度 |

### 技术与架构

| 文档 | 说明 |
|------|------|
| [docs/04-architecture.md](./docs/04-architecture.md) | 分层架构、数据流、反模式 |
| [docs/05-stack-selection.md](./docs/05-stack-selection.md) | 工具栈选型与组合（Multica / LangGraph / Cursor / CI） |
| [docs/06-task-grading.md](./docs/06-task-grading.md) | 任务分级：`agent-safe` / `assisted` / `human-only` |
| [docs/07-quality-gates.md](./docs/07-quality-gates.md) | 硬门禁规范：退出码、契约、E2E |
| [docs/08-multi-project-portfolio.md](./docs/08-multi-project-portfolio.md) | 多项目 / 多站点组合管理 |
| [docs/09-compliance-and-risk.md](./docs/09-compliance-and-risk.md) | 出海合规、安全、审计 |
| [docs/10-cost-and-budget.md](./docs/10-cost-and-budget.md) | 成本模型与预算护栏 |
| [docs/11-langgraph-when-and-how.md](./docs/11-langgraph-when-and-how.md) | 何时上 LangGraph、如何与 Multica 对接 |
| [docs/12-ceo-dashboard.md](./docs/12-ceo-dashboard.md) | 老板仪表盘：看什么、何时介入 |
| [docs/17-ceo-cockpit.md](./docs/17-ceo-cockpit.md) | **CEO 指挥舱**：脱手 + 一屏总览（`:9477`）架构与分阶段 |
| [docs/27-norm-sync.md](./docs/27-norm-sync.md) | **规范同步**：三层管道 + `sync-company-norms.sh` |
| [docs/28-norm-layers.md](./docs/28-norm-layers.md) | **规范分层**：通用 vs 项目 vs 任务 |
| [docs/29-harness-layout.md](./docs/29-harness-layout.md) | **Harness 布局**：按类型放哪、examples vs templates、multica 本仓 |
| [docs/30-silicon-valley-doc-standards.md](./docs/30-silicon-valley-doc-standards.md) | **硅谷文档规范**：SSOT、英文代码面、Issue=Spec、语言分工 |
| [docs/31-harness-learnings-routing.md](./docs/31-harness-learnings-routing.md) | **Harness 经验回流**：关键词路由 + 候选队列 |
| [docs/32-opc-harness-knowledge-design.md](./docs/32-opc-harness-knowledge-design.md) | **设计方案总览**：知识联邦 + Tier-0 + 硅谷差距（HQ） |
| [docs/18-definition-of-done.md](./docs/18-definition-of-done.md) | **DoD** 完成定义 |
| [docs/20-issue-brief-style-guide.md](./docs/20-issue-brief-style-guide.md) | Issue / brief 写作规范 |
| [docs/21-label-state-machine.md](./docs/21-label-state-machine.md) | Label 状态机与 BLOCKED 原因码 |
| [docs/22-git-and-remotes.md](./docs/22-git-and-remotes.md) | Git / fork / 产品仓推送 |
| [docs/23-local-agent-environment.md](./docs/23-local-agent-environment.md) | 本机 Agent 环境（pnpm、代理、cron） |
| [docs/19-asset-registry.md](./docs/19-asset-registry.md) | **资产台账**：registry / 路径 / 域名 |
| [docs/13-implementation-roadmap.md](./docs/13-implementation-roadmap.md) | P0→P3 分阶段路线图 |
| [docs/14-multica-autopilot-portfolio.md](./docs/14-multica-autopilot-portfolio.md) | **Portfolio 夜间调度 + Multica Autopilot** |
| [docs/15-feishu-site-factory.md](./docs/15-feishu-site-factory.md) | **飞书一句话建站：竞品 → MVP → Cloudflare → 多 Agent** |
| [docs/24-content-operations.md](./docs/24-content-operations.md) | **自媒体线：远程 Hermes + registry + 派单** |
| [docs/16-disaster-recovery.md](./docs/16-disaster-recovery.md) | **灾备策略摘要：GitHub 放什么 / 密钥不放什么** |
| [docs/system-evolution/README.md](./docs/system-evolution/README.md) | **系统进化**（周报目录，真相源在 Multica） |

### 运营 Runbook

| 文档 | 说明 |
|------|------|
| [runbooks/feishu-one-line-site.md](./runbooks/feishu-one-line-site.md) | **飞书「做一个 XX 网站」建站 runbook** |
| [runbooks/ceo-daily.md](./runbooks/ceo-daily.md) | CEO 每日 15 分钟 |
| [runbooks/nightly-ceo-brief.md](./runbooks/nightly-ceo-brief.md) | **每晚 21:00 自动派单 + 日报** |
| [runbooks/ceo-weekly.md](./runbooks/ceo-weekly.md) | CEO 每周治理 |
| [runbooks/onboard-new-project.md](./runbooks/onboard-new-project.md) | 新网站 / 新项目接入清单 |
| [runbooks/blocked-triage.md](./runbooks/blocked-triage.md) | BLOCKED 分拣与澄清 |
| [runbooks/incident-response.md](./runbooks/incident-response.md) | CI 挂、泄露、合规事件 |
| [runbooks/disaster-recovery.md](./runbooks/disaster-recovery.md) | **换机 / 盘挂：恢复顺序 + Host Inventory** |
| [runbooks/employee-autopilot.md](./runbooks/employee-autopilot.md) | 白天员工 Autopilot |
| [runbooks/work-finder.md](./runbooks/work-finder.md) | **找活工：队列薄时自己造 agent-safe 票** |

### 模板与配置

| 文件 | 说明 |
|------|------|
| [templates/project-brief.md](./templates/project-brief.md) | 项目 brief 模板 |
| [templates/CLAUDE.project.md](./templates/CLAUDE.project.md) | **项目 CLAUDE.md 骨架**（产品仓根目录） |
| [templates/orchestrator-kickoff-product.md](./templates/orchestrator-kickoff-product.md) | 产品仓 Agent kickoff（install 写入 prompts/） |
| [templates/accept_cases.md](./templates/accept_cases.md) | 验收用例模板 |
| [templates/api_spec.openapi.yaml](./templates/api_spec.openapi.yaml) | OpenAPI 契约骨架 |
| [templates/merge-policy.json](./templates/merge-policy.json) | auto-merge 白名单模板 |
| [templates/project-registry.yaml](./templates/project-registry.yaml) | 公司项目台账 |
| [config/company-defaults.yaml](./config/company-defaults.yaml) | 公司级默认参数 |
| [config/company-assets.local.yaml.example](./config/company-assets.local.yaml.example) | 本机资产登记模板（域名、Secret 名） |

### 可复制脚手架与示例

| 路径 | 说明 |
|------|------|
| [docs/29-harness-layout.md](./docs/29-harness-layout.md) | **按类型索引**：docs / examples / harness / multica 本仓 |
| [harness/HARNESS-INDEX.md](./harness/HARNESS-INDEX.md) | Harness 目录快捷入口 |
| [examples/README.md](./examples/README.md) | **种子包索引**（复制到 `.delivery/<slug>/`） |
| [harness/](./harness/) | **company-harness**：`install.sh` 一键装进任意 repo |
| [examples/music-game-sea/](./examples/music-game-sea/) | 出海音乐游戏站 **完整 brief + AC + API + backlog** |
| [examples/landing-tool-a/](./examples/landing-tool-a/) | 第二产品线：轻量工具落地页（4 ticket） |
| [examples/saas-stripe-mvp/](./examples/saas-stripe-mvp/) | 第三产品线：SaaS 壳（支付 human-only） |
| [../scripts/ai-company/bootstrap-project.sh](../scripts/ai-company/bootstrap-project.sh) | **一键**：harness + labels + 建 repo + 灌 backlog |
| [../scripts/ai-company/portfolio-dispatch.sh](../scripts/ai-company/portfolio-dispatch.sh) | 按 registry 多仓夜间 dispatch |
| [../scripts/ai-company/scaffold-landing.sh](../scripts/ai-company/scaffold-landing.sh) | 生成 `landing-tool-a` |
| [../scripts/ai-company/ceo-dashboard.sh](../scripts/ai-company/ceo-dashboard.sh) | **CEO 一条命令看全公司状态** |
| [../scripts/ai-company/ceo-daily-brief.sh](../scripts/ai-company/ceo-daily-brief.sh) | 生成 CEO 日报 markdown + webhook |
| [../scripts/ai-company/ceo-nightly.sh](../scripts/ai-company/ceo-nightly.sh) | 每晚派单 + 日报（cron 入口） |
| [../scripts/ai-company/install-nightly-cron.sh](../scripts/ai-company/install-nightly-cron.sh) | 安装 21:00 crontab |
| [../scripts/ai-company/bootstrap-all-products.sh](../scripts/ai-company/bootstrap-all-products.sh) | 批量 bootstrap 三条产品线 |
| [../scripts/ai-company/install-harness.sh](../scripts/ai-company/install-harness.sh) | 从仓库根调用的安装包装脚本 |
| [../scripts/ai-company/sync-backlog-to-issues.sh](../scripts/ai-company/sync-backlog-to-issues.sh) | 从 `backlog.md` 批量创建 GitHub Issue |
| [../scripts/ai-company/sync-company-norms.sh](../scripts/ai-company/sync-company-norms.sh) | **把选定 `.ai-company/` 规范复制到各产品 `.delivery/company-os/`** |

---

## 四阶段落地（从 0 到躺平）

| 阶段 | 目标 | 主要文档 |
|------|------|----------|
| **P0 单工厂** | 1 个项目 Issue→PR→CI 绿 | `onboard-new-project` + `.delivery/` |
| **P1 多项目** | 复制 harness，Autopilot 扫队列 | `08-multi-project` + `project-registry` |
| **P2 硬编排** | 队列变大，上 LangGraph 细粒度 DAG | `11-langgraph-when-and-how` |
| **P3 公司化** | 固定编制、成本护栏、合规双层 | 全文 |

---

## 快速开始（CEO 第一次）

1. 读 [docs/01-vision.md](./docs/01-vision.md) 与 [docs/02-operating-model.md](./docs/02-operating-model.md)（10 分钟）。
2. 按 [runbooks/onboard-new-project.md](./runbooks/onboard-new-project.md) 接入第一个站。
3. 复制 [templates/](./templates/) 到该项目仓库的 `.delivery/<slug>/`，或直接用 [examples/music-game-sea/](./examples/music-game-sea/)。
4. 运行 `bash .ai-company/harness/install.sh /path/to/project` 安装 harness。
4. 自托管 [Multica](https://github.com/multica-ai/multica) 或先用 GitHub Actions 路径 B（见 `.delivery/README.md`）。
5. 每日按 [runbooks/ceo-daily.md](./runbooks/ceo-daily.md) 操作。

---

## 硬规则（全公司）

1. **只有编排层（LangGraph / GHA / 脚本）能决定流程下一步**；Agent 只产出工件。
2. **CI 不绿 = 未交付**；禁止口头「应该没问题」。
3. **Hermes / OpenClaw 只做 Worker**，禁止做顶层调度；无人值守流水线关闭 Hermes 自进化。
4. **合规与支付：脚本门禁优先，Agent 评审辅助**。
5. **新项目必须复制 harness**，不得每站一套 Prompt 野路子。

---

## 与 Multica 仓库现有资产

| 资产 | 路径 | 用途 |
|------|------|------|
| 交付流水线实例 | `.delivery/` | Multica 本仓 dogfood |
| 子 Agent 定义 | `.cursor/agents/` | Planner / Implementer / Verifier / Reviewer |
| GHA 调度 | `.github/workflows/agent-delivery-*.yml` | 硬调度 + merge 门禁 |
| 调度脚本 | `scripts/agent-delivery/` | dispatch / gate-check |

新公司项目应 **fork 或 submodule 复制** 上述 harness，再填项目级 brief。

| [docs/13-opc-bridge.md](./docs/13-opc-bridge.md) | OPC（SecondBrain 一人公司）↔ Company OS 桥接 |
