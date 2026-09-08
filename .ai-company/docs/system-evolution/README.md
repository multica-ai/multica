# System Evolution（系统进化）

公司级「让员工更好自己干活」的沉淀目录——**真相源在 Multica**，不在飞书工作区。

| 节奏 | 产出 |
|------|------|
| 心跳（~30min） | 短想；低成本改直接做 |
| 每周一 10:00 | `YYYY-MM-DD-weekly.md` 一页纸建议 |
| harness 候选 | [harness-candidates.md](./harness-candidates.md) 升格 ≤3 条/周 |

- 架构级改动：等 CEO 批准  
- 笔误/文档级：可直接做并写进当周报告  
- **Harness 经验**：路由见 [31-harness-learnings-routing.md](../31-harness-learnings-routing.md)；Agent 只写候选队列，CEO 周回顾升格到 `docs/` / `brief` / Vault HARNESS

Runbook 交叉：`../runbooks/employee-autopilot.md`

## 周回顾模板（复制到新文件 `YYYY-MM-DD-weekly.md`）

```markdown
# 系统进化周回顾 — YYYY-MM-DD

## 本周事实
- 派 / 绿 / 堵：（数字）

## Harness 候选升格（≤3）
| 候选摘要 | 升格到 | 状态 |
|----------|--------|------|
| … | docs/… | promoted / task-only |

## 最大摩擦（1 个）

## 建议改动（≤3）

## 下周一验收点
```

验收：`bash scripts/ai-company/verify-harness-learnings.sh`
