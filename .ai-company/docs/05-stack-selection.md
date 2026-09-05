# 05 — 工具栈选型

## 总表（公司标准栈）

| 层级 | 首选 | 备选 | 不做顶层 |
|------|------|------|----------|
| 任务公司 OS | **Multica**（自托管） | Linear + 自建脚本 | — |
| 编排内核 P1 | **GitHub Actions** + shell | Cursor Automations | Hermes / OpenClaw |
| 编排内核 P2 | **LangGraph** | Temporal / 自写状态机 | LLM prompt DAG |
| 编码 Worker | **Cursor Business** (`cursor-agent`) | Claude Code, Codex | — |
| 安全评审 Worker | Hermes（只评审） | 专用 SAST 脚本 | Hermes 调度 |
| 情报 Worker | OpenClaw | 自建爬虫 | OpenClaw 调度 |
| API 契约 | **vacuum** + **oasdiff** | Spectral, Redocly | 纯 Agent 判断 |
| E2E | **Playwright** | Cypress | 无 E2E 上线 |
| 告警 | Slack webhook | 邮件 | 无告警躺平 |
| Spec 变更 | OpenSpec（可选） | 纯 markdown brief | — |

---

## 为什么这样选

### Multica — 任务公司层

- 原生 `cursor-agent`，Issue 生命周期，Autopilot cron/webhook。
- 多 runtime 并行，活动日志，自托管可审计。
- **缺口**：无编程式细粒度 DAG → 由 L2 补。

### CEO 本机 cron — P1 编排

- `ceo-nightly.sh` → `portfolio-dispatch.sh`（本机 `cursor-agent`）。
- `agent-delivery-gate.yml`：`check-merge-eligible.sh` 路径白名单（GHA，仅 merge 门禁）。
- **只信 exit code**；与 Multica 路径 C 可并存。

### LangGraph — P2 编排（队列变大后）

- checkpoint、条件边、`interrupt()` HITL。
- Python 读 `subprocess` 退出码分支。
- 见 [11-langgraph-when-and-how.md](./11-langgraph-when-and-how.md)。

### Cursor — 主力 Worker

- 本机 `cursor-agent` CLI（`cursor-agent login`，session auth）。
- Business 版支持无人值守批量（CEO 机器 21:00 cron + 预算护栏）。

### 契约工具链

```bash
# PR 门禁示例
vacuum lint api/openapi.yaml --fail-severity error
oasdiff breaking origin/main:api/openapi.yaml HEAD:api/openapi.yaml --fail-on WARN
```

推荐参考：[api-commons/governance-pipeline](https://github.com/api-commons/governance-pipeline)。

---

## 账号与许可证清单

| 产品 | 用途 | 备注 |
|------|------|------|
| Cursor Business | 本机 `cursor-agent` CLI（session） | `cursor-agent login` |
| GitHub Team/Enterprise | Actions, branch protection | required checks |
| Multica 自托管 | 任务看板 | Docker / Helm |
| Slack | 告警 | `SLACK_WEBHOOK_URL` |
| 云主机（可选） | daemon runtime | 与代码同区域 |

---

## 开源参考（不整包替换，只抄模式）

| 项目 | 抄什么 |
|------|--------|
| multica-ai/multica + `.delivery/` | 本公司实例 |
| peter-stratton/dark-factory | 双评审 + scenario gate |
| sdageltc/letitloop | 机器验退出码 CONTRACT |
| makshc2/agent-orchestrator-kit | `gate-check` CI 模式 |

---

## 升级触发条件

| 信号 | 动作 |
|------|------|
| 队列 >10 agent-safe ticket/天 | 评估 LangGraph |
| 单任务需「只修失败用例」 | 上 LangGraph 细粒度边 |
| 多 CEO 协作 | Multica 多 workspace + CODEOWNERS |
| 成本超预算 80% | 降并发、缩模型、缩 E2E 范围 |

---

## 相关文档

- [04-architecture.md](./04-architecture.md)  
- [10-cost-and-budget.md](./10-cost-and-budget.md)  
