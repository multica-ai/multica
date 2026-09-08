# 21 — Label 状态机与 BLOCKED 原因码

> **层级**：通用  
> **GitHub Labels** 与 [02-operating-model.md](./02-operating-model.md) 一致；本文为 **转移规则 + Agent 输出格式**。

---

## Labels 一览

| Label | 含义 | 谁打 |
|-------|------|------|
| `agent-safe` | 允许自主处理 | CEO / triage |
| `agent-assisted` | 可 Agent 出 PR，CEO 必 merge | triage |
| `human-only` | 禁止进 Agent 队列 | triage |
| `agent-running` | 处理中 | dispatch / orchestrator |
| `agent-blocked` | 停止，等 CEO | Planner / Verifier / policy |
| `agent-done` | PR 开且 CI 绿 | gate / CEO |

夜间 cron **只拉**：

```text
label:agent-safe -label:agent-running -label:agent-blocked -label:agent-done
```

---

## 状态转移图

```text
                    ┌─────────────┐
         triage     │ agent-safe  │◄────────────────┐
        ──────────► │  (queued)   │                 │
                    └──────┬──────┘                 │
                           │ dispatch               │ CEO 澄清后
                           ▼                        │ remove blocked
                    ┌─────────────┐                 │ re-add safe
                    │agent-running│                 │
                    └──────┬──────┘                 │
           ┌───────────────┼───────────────┐         │
           ▼               ▼               ▼         │
    ┌────────────┐   ┌────────────┐  ┌──────────┐   │
    │agent-done  │   │agent-blocked│  │(strip    │   │
    │ PR+CI绿   │   │             │  │ running) │   │
    └────────────┘   └──────┬──────┘  └──────────┘   │
                            │                        │
                            └────────────────────────┘
```

---

## 转移表（规范）

| 从 | 到 | 触发 | 禁止 |
|----|-----|------|------|
| — | `agent-safe` | triage 通过 | 未分级 |
| `agent-safe` | `agent-running` | dispatch 选中 | 已有 running/blocked |
| `agent-running` | `agent-done` | PR + CI 绿 | 无 PR 就打 done |
| `agent-running` | `agent-blocked` | 见原因码 | 继续耗配额 |
| `agent-blocked` | `agent-safe` | CEO 更新 brief/Issue 并澄清 | **不更新 brief 就重跑** |
| `agent-done` | — | 终态（可关 Issue） | 回退到 safe 派同一 PR |
| 任意 | `human-only` | CEO 降级 | Agent 继续派单 |

**并发：** 同一 Issue 不得同时 `agent-running` 且被二次 dispatch（`pick_next_issue` 应跳过）。

---

## BLOCKED 原因码（Agent 必须输出）

Planner / Verifier / orchestrator 停止时，在 Issue comment 或 `blocked.md` **首行**写：

```text
BLOCKED:<CODE>

<one-line summary>

<details if needed>
```

| CODE | 含义 | CEO 动作 |
|------|------|----------|
| `NEED_CLARIFY` | 需求歧义 | 编号回答 → 更新 brief/Issue |
| `VERIFY_EXHAUSTED` | 3 轮 verify 仍失败 | 看日志；拆票 / 修 infra / human-only |
| `POLICY_DENY` | 改动命中 merge-policy deny | 拆票或改 human-only |
| `INFRA` | 环境/配额/registry/依赖 | 修本机或 CI；见 [23](./23-local-agent-environment.md) |
| `MISSING_DOD` | 无 AC / 无 inventory+wont_do | 补 Issue 或 brief |
| `SCOPE_CREEP` | 超出 brief/Issue | 收窄或新 Issue |

示例：

```markdown
BLOCKED:NEED_CLARIFY

1. footer 链接是否必须外链？→ 是/否
2. 移动端是否要与 desktop 同文案？
```

---

## `agent-done` 何时打

**全部满足：**

- [ ] PR 已开（head 通常 `cursor/*`）
- [ ] Required CI checks 绿
- [ ] Verifier 证据在 PR body
- [ ] 非 assisted 路径，或 CEO 已准备 merge

**不打 done：** CI 黄、仅本地通过、policy deny。

---

## reconcile 修复（脚本）

`ceo-reconcile-queue.sh` 等可修正 **陈旧 label**（如 PR 已 merge 仍 running）。  
手工原则与上表一致，不发明新状态。

---

## 相关文档

- [02-operating-model.md](./02-operating-model.md)  
- [runbooks/blocked-triage.md](../runbooks/blocked-triage.md)  
- [18-definition-of-done.md](./18-definition-of-done.md)  
