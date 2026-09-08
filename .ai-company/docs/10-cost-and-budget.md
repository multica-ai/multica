# 10 — 成本与预算

## 成本构成

| 类别 | 计费点 | 控制手段 |
|------|--------|----------|
| **LLM / Cursor Cloud** | 每 ticket Agent 运行 | `max_nightly_tickets`、分级 |
| **CI** | Actions 分钟、E2E 时长 | 路径过滤、smoke vs full E2E |
| **基础设施** | Multica 自托管、DB、runtime 机 | 单实例多项目 |
| **人工** | CEO BLOCKED 时间 | 提高 AC 质量、减少模糊 brief |

---

## 公司默认护栏

见 [config/company-defaults.yaml](../config/company-defaults.yaml)：

```yaml
budget:
  monthly_cursor_usd: 500        # 示例，按实际调整
  alert_threshold_percent: 80
  pause_autopilot_on_exceed: true

concurrency:
  max_parallel_cloud_agents: 3
  max_verify_loops: 3

nightly:
  max_total_tickets: 5
  cron: "0 21 * * *"             # 21:00 Asia/Shanghai
  timezone: Asia/Shanghai
```

**运行时**：`scripts/ai-company/lib/budget-guard.sh` 在 `autopilot-dispatch.sh` 派单前读取上述配置；用量来自 `~/.multica/budget-state.json`（按 dispatch 估算）或环境变量 `AUTOPILOT_MONTHLY_SPEND_USD`（手填 Cursor 账单）。

---

## 单 ticket 成本优化

1. **Verifier 先跑窄测试**，全量 `make check` 仅 PR 前一次。
2. **E2E 在 CI 跑**，Agent 环只跑 smoke。
3. **拆小 ticket** — 失败重试成本线性下降。
4. **deny 路径** 避免 Agent 改大面 API 导致多轮 CI。

---

## 仪表盘指标

| 指标 | 用途 |
|------|------|
| $/merged PR | 效率 |
| $/agent-blocked | brief 质量 |
| 首次 CI 通过率 | harness 健康度 |
| 夜间 ticket 数 | 容量规划 |

---

## 超预算 playbook

1. 告警触发 → Autopilot `paused`（Multica 支持批量暂停）。
2. 仅保留 P0 生产项目队列。
3. 下周 CEO 周会：砍 experiment 配额或升预算。

---

## 相关文档

- [12-ceo-dashboard.md](./12-ceo-dashboard.md)  
- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md)  
