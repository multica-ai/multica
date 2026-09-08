# 12 — CEO 仪表盘

> **界面战略**：脱手靠 cron + 飞书推；想「一眼看全公司（资产 / 规范 / 流程）」只开指挥舱 `:9477`。详见 [17-ceo-cockpit.md](./17-ceo-cockpit.md)。

## 每日必看（一处汇总）

| 信号 | 去哪看 | 绿色 | 红色 |
|------|--------|------|------|
| 昨夜交付 | GitHub merged PRs（`cursor/*`） | 有 merge | 零交付且 backlog 堆积 |
| 阻塞 | Issues `label:agent-blocked` | 0 | ≥1 → 立即处理 |
| CI 健康 | 各 repo Actions | 全绿 | 连续失败 |
| 成本 | Cursor dashboard / 自建表 | <80% 预算 | ≥80% |
| 队列深度 | `agent-safe` open count | 下降趋势 | 持续增长 |

---

## Multica 视图

- **Issues 看板**：按 `agent-*` label 列。
- **Tasks**：失败原因、`attempt`、runtime 日志。
- **Autopilot runs**：昨夜是否触发、跳过原因。

---

## GitHub 视图

```bash
# 阻塞
gh issue list -R org/repo -l agent-blocked

# 运行中
gh issue list -R org/repo -l agent-running

# 昨夜 Agent PR
gh pr list -R org/repo --search "head:cursor/ merged:>@yesterday"
```

多 repo：脚本读 `project-registry.yaml` 循环。

**已实现：** `scripts/ai-company/ceo-dashboard.sh`（终端）与 `scripts/ai-company/ceo-workbench.sh`（浏览器工作台）

每晚 21:00：`ceo-nightly.sh`（派单 + `ceo-daily-brief.sh` + webhook），见 [runbooks/nightly-ceo-brief.md](../runbooks/nightly-ceo-brief.md)。

```bash
bash scripts/ai-company/ceo-workbench.sh
bash scripts/ai-company/ceo-dashboard.sh
bash scripts/ai-company/ceo-dashboard.sh --json
bash scripts/ai-company/ceo-dashboard.sh --dispatch --max-total 3
bash scripts/ai-company/install-nightly-cron.sh --install
```

---

## 何时深入（打破躺平）

| 触发 | CEO 动作 |
|------|----------|
| 同一类 BLOCKED ≥3 次/周 | 修 harness 或 brief 模板 |
| CI 首次通过率 <50% | Verifier 命令与 CI 对齐检查 |
| 成本飙升 | 缩 `max_nightly_tickets` |
| 生产 incident | incident runbook；暂停 autopilot |

**默认不打开 PR diff。**

---

## 周报模板（复制到 Notion/飞书）

```markdown
## AI 公司周报 YYYY-Www

### 交付
- Merged PRs: N（按项目分列）
- agent-safe 完成率: X%

### 阻塞
- BLOCKED 新增/关闭: a/b
-  Top 原因: …

### 质量
- CI 首次通过率: X%
- 契约 breaking 拦截: N 次

### 成本
- Cursor 花费: $X / 预算 $Y

### 下周
- 新项目接入: …
- harness 改动: …
```

---

## 相关文档

- [17-ceo-cockpit.md](./17-ceo-cockpit.md) — 指挥舱架构与分阶段路线图  
- [runbooks/ceo-daily.md](../runbooks/ceo-daily.md)  
- [runbooks/ceo-weekly.md](../runbooks/ceo-weekly.md)  
