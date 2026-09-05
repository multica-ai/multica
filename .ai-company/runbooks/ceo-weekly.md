# CEO 每周 Runbook（30 分钟）

## 1. 指标回顾

填 [12-ceo-dashboard.md](../docs/12-ceo-dashboard.md) 周报模板：

- Merged PRs / 项目
- agent-safe 完成率
- BLOCKED 原因 Top3
- CI 首次通过率
- 成本 vs 预算

## 2. 项目台账

更新 [templates/project-registry.yaml](../templates/project-registry.yaml)：

- 优先级排序
- `max_nightly_tickets` 调整
- 暂停/恢复 experiment 项目

## 3. Harness 健康

| 检查 | 动作 |
|------|------|
| 同一 BLOCKED 原因重复 | 修模板 / verifier 命令 |
| CI 与 Verifier 命令不一致 | 对齐 `accept_cases` |
| merge-policy 过宽 | 收紧 deny |

## 4. 分级审计

随机抽 5 个已 merge 的 agent-safe PR：

- 是否真的满足 [06-task-grading.md](../docs/06-task-grading.md)？
- 违规 → 下周收紧 triage

## 5. 战略投放

- 下周要上线/实验的 **1～2 个 brief** 写入对应 `.delivery/<slug>/brief.md`
- 大需求 **拆 ticket** 后再进队列

## 6. Autopilot / Cron

- 确认夜间 cron 启用且 Secrets 未过期
- Multica Autopilot run 历史无静默失败

---

## 月度附加（每第 1 个周一）

- 复审 [09-compliance-and-risk.md](../docs/09-compliance-and-risk.md)
- 轮换 API Key（如政策要求）
- 评估是否上 LangGraph（见 [11-langgraph-when-and-how.md](../docs/11-langgraph-when-and-how.md)）
