# 13 — 分阶段实施路线图

## 总览

```text
P0 单工厂 ──► P1 多项目 ──► P2 硬编排 ──► P3 公司化
 (1 站)      (N 站复制)    (LangGraph)   (成本/合规/编制固化)
```

---

## P0 — 单工厂（第 1～2 周）

**目标：** 一个项目，Issue → PR → CI 绿，CEO 日操 15min。

| 交付物 | 验收 |
|--------|------|
| harness 接入 | trivial ticket 跑通 |
| GHA dispatch + gate | auto-merge 行为符合 policy |
| CEO daily runbook | 可执行无歧义 |

**不做：** LangGraph、多 repo 矩阵、Hermes/OpenClaw。

---

## P1 — 多项目组合（第 3～6 周）

**目标：** ≥3 个项目共用 harness；夜间队列公平调度。

| 交付物 | 验收 |
|--------|------|
| `project-registry.yaml` 实盘 | 每项目有 priority/cap |
| 第二、第三站接入 | 各至少 1 merge |
| Multica Autopilot（推荐） | run 历史可查 |
| CEO weekly runbook | 连续 4 周执行 |

**不做：** 细粒度用例级回退。

### P1.5 — CEO 指挥舱（与 P1 并行，推荐 1～2 周）

**目标：** 脱手已成立的前提下，`:9477` 成为唯一 **拉** 界面（资产 + 规范入口 + 流程灯 + 队列脉搏）；飞书仍是唯一 **推** 界面。

| 交付物 | 验收 |
|--------|------|
| workbench「公司概览」 | registry 表 + 规范链 + verify / nightly 灯 + 资产列（`:9477` 已实现） |
| 界面分工文档冻结 | [17-ceo-cockpit.md](./17-ceo-cockpit.md) |

**不做：** Multica UI 替代指挥舱；GitHub label 双向同步。

详见 [17-ceo-cockpit.md](./17-ceo-cockpit.md) 阶段 0～3。

---

## P2 — 硬编排（按需，通常第 2～3 月）

**触发：** 日 ticket >10 或 verify 整包重试成本过高。

| 交付物 | 验收 |
|--------|------|
| LangGraph 图 + checkpoint | 测试失败分支由 Python 决定 |
| Multica webhook 接缝 | 任务状态双向可见 |
| HITL interrupt | BLOCKED 可 resume |

---

## P3 — 公司化（持续）

| 交付物 | 验收 |
|--------|------|
| 成本仪表盘 | 超 80% 自动暂停 |
| 合规脚本库 | 出海站 CI 必跑项 |
| `company-harness` 独立 repo | 新项目 template 一键 |
| 编制文档冻结版 | 季度复审 |

---

## 风险与降级

| 风险 | 降级 |
|------|------|
| Cursor API 不稳定 | 降并发；白天手动 dispatch |
| Agent 频繁 BLOCKED | 收紧分级；改 brief 模板 |
| 成本失控 | `pause_autopilot_on_exceed` |
| 质量事故 | 收紧 merge-policy；human-only 扩面 |

---

## 成功画面（P1 结束）

> 你有 3～5 个站同时在跑；每晚 Autopilot 处理 agent-safe 队列；  
> 早上 15 分钟：处理 0～2 条 BLOCKED、勾 AC、投新 brief；  
> 不读代码；CI 不绿的不存在。

这就是「AI 公司老板躺平」的 **P1 现实版**。P2/P3 在此基础上更省心与更可审计。
