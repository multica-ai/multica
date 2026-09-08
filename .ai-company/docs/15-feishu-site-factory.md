# 15 — 飞书一句话建站（Site Factory）

> 在飞书发「做一个 XX 网站」→ 自动竞品调研 → MVP → Cloudflare 脚手架 → 多 Agent 交付。

## 能力概览

| 阶段 | 执行者 | 产出 |
|------|--------|------|
| 1 解析 | `site-factory.sh` | slug、目录、流水线日志 |
| 2 竞品调研 | cursor-agent（research 模板） | `.delivery/<slug>/research.md`（含 inventory 种子 / bot 墙说明） |
| 3 MVP 定义 | cursor-agent（mvp 模板） | `brief.md` / `competitor_inventory.md` / `wont_do.md` / `accept_cases.md` / `backlog.md` |
| 4 脚手架 | `scaffold-cloudflare.sh` | Cloudflare Pages + Vite + Playwright `@visual` harness |
| 5 接入 | `bootstrap-project.sh` | GitHub labels、Issues |
| 6 登记 | `project-registry.yaml` | 纳入夜间组合调度 |
| 7 派单 | `dispatch-cursor-agent-cli.sh` | Planner→Implementer→Verifier 流水线 |

**完整度门禁（Visual Replica Gate）：** MVP 阶段必须产出 inventory + wont_do；Verifier 对复刻/落地页必须跑 `make visual-check`。详见 [07-quality-gates.md](./07-quality-gates.md) L4-视觉 与 [runbooks/visual-replica-gate.md](../runbooks/visual-replica-gate.md)。

**栈约束：** 仅 Cloudflare（Pages + Workers + Wrangler）。不用 Vercel、不用生产 Docker。

**部分服务** 通过本机 **Multica 自托管运行栈**（`make selfhost` / `local-selfhost-autostart.sh`）与 **multica daemon** 提供：API 健康检查、Agent 并发上限、派单槽位计算。CEO 工作台（`:9477`）负责 intake 与 job 日志。

| 运行服务 | 用途 |
|----------|------|
| Multica API `/readyz` | 流水线启动前健康检查 |
| `multica daemon` | Agent 任务并发与 runtime |
| `multica-runtime-status.sh` | 派单槽位 / 工作台可观测 |
| CEO workbench `:9477` | 飞书/API 统一 intake |
| `lib/site-factory-multica.sh` | 可选：Multica issue 分阶段派单（需 `SITE_FACTORY_MULTICA_AGENT_ID` 或 idle local agent） |

派单默认 **auto**：daemon 在线时先试 Multica，失败则回退 `dispatch-cursor-agent-cli.sh`（Orchestrator 多 Agent 流水线）。CLI 派单会按 GitHub **默认分支**（如 `master`）建 worktree。

---

## 飞书触发

### 方式 A — 自然语言（推荐）

私聊 Bot 或群聊 `@Bot`：

```text
做一个 JSON 格式化网站
建一个 No WiFi 工具站点
site-factory: 海外 SEO 关键词计数器
```

`feishu-cursor-claw` 识别后后台执行 `site-factory.sh --background --notify`。

### 方式 B — 显式路由

```text
multica: site-factory --intake "做一个 XX 网站" --create-repo
```

### 方式 C — CEO 工作台 API

```bash
curl -sS -X POST http://127.0.0.1:9477/api/site-factory \
  -H 'Content-Type: application/json' \
  -d '{"intake":"做一个 XX 网站","create_repo":true,"notify":true}'
```

---

## CLI（本机）

```bash
# 预览计划
bash scripts/ai-company/site-factory.sh \
  --intake "做一个 JSON 格式化网站" --dry-run

# 全流水线（含 agent 调研 + MVP，不含 gh create）
bash scripts/ai-company/site-factory.sh \
  --intake "做一个 JSON 格式化网站" --notify

# 创建 GitHub 仓库 + push + 同步 backlog + 本地派 2 票
bash scripts/ai-company/site-factory.sh \
  --intake "做一个 XX 网站" --create-repo --max-dispatch 2 --notify
```

日志：`.ai-company/run-logs/site-factory-<slug>-*.log`  
活跃流水线：`.ai-company/run-logs/active-pipelines.txt`

---

## 前置条件

- `cursor-agent login`（research / MVP / dispatch 阶段）
- `feishu-cursor-claw` 常驻（方式 A）或 CEO workbench（方式 C）
- `setup-feishu-bot-notify.sh` 或 webhook（`--notify`）
- 可选：`gh auth login`（`--create-repo`）

验证：

```bash
bash scripts/ai-company/site-factory-verify.sh
bash scripts/ai-company/feishu-site-factory-smoke.sh
bash scripts/ai-company/site-factory.sh --intake "测试站点" --dry-run
```

**Live 验收（Goal 最后一项）：** 飞书私聊 Bot 发「做一个 XX 网站」→ 收到「建站流水线已提交 CEO 工作台」卡片 → 查 `~/.multica/ceo-workbench/jobs/` 日志。

---

## 自迭代如何开启

新建项目（`--create-repo`）时**会询问是否接入自迭代**：

| 场景 | 行为 |
|------|------|
| **CLI 交互** | 提示 `接入自迭代? [Y/n]`，默认 **Y** |
| **CEO 工作台** | 勾选「创建 GitHub 仓库」后出现「接入自迭代」复选框，默认 **勾选** |
| **飞书 / 后台** | 非交互默认 **不接入**；需显式 `--activate-autopilot` 或工作台勾选 |

接入自迭代 = 登记 `project-registry.yaml` + 写入 `repo-paths.local.yaml` + 派首张 `agent-safe` 票。  
不接入时仍会 `bootstrap` GitHub Issues，可稍后补开：

```bash
bash scripts/ai-company/activate-project-autopilot.sh --id <slug> --target ~/Projects/<slug>
```

已有项目（如 **MeiGen AI / `meigen-replica`**）一次性激活：

```bash
bash scripts/ai-company/activate-project-autopilot.sh \
  --id meigen-replica \
  --target ~/Projects/meigen-replica \
  --repo chenzh/meigen-replica \
  --stack visual-replica \
  --from TICKET-001 --to TICKET-003
```

之后白天 **Employee Autopilot** 会自动派单（无需 CEO 每次喊「派单」）：

| 入口 | 命令 / URL |
|------|------------|
| 自动（推荐） | `autopilot-dispatch.sh`（LaunchAgent/cron 已装则后台跑） |
| 手动一刀 | `bash scripts/ai-company/autopilot-dispatch.sh --force` |
| CEO 工作台 | http://127.0.0.1:9477 → 项目队列 →「派这一单」 |
| 单票 | `bash scripts/agent-delivery/dispatch-cursor-agent-cli.sh <issue#>` |

**前提检查：**

```bash
cursor-agent status
bash scripts/ai-company/resolve-repo-path.sh --id meigen-replica
bash scripts/ai-company/multica-runtime-status.sh
gh issue list -R chenzh/meigen-replica -l agent-safe
```

暂停自迭代：在 `project-registry.yaml` 把该项目设为 `paused: true`。

---

## CEO 仍须负责

- BLOCKED / NEED_CLARIFY 答复
- `accept_cases.md` 最终勾选
- 支付、合规、生产 Cloudflare 账号绑定（human-only）

---

## 相关

- [runbooks/feishu-one-line-site.md](../runbooks/feishu-one-line-site.md)
- [04-architecture.md](./04-architecture.md)
- [examples/cloudflare-site/](../examples/cloudflare-site/)
