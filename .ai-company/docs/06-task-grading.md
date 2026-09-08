# 06 — 任务分级

所有进入 AI 公司的工单必须先分级。**未分级不得进夜间队列。**

---

## 三级分类

### ✅ `agent-safe` — 全自动（睡眠模式唯一入口）

**全部满足才可打标：**

- [ ] 有 **可勾选** `accept_cases.md`（或 Issue 模板 AC 列表）
- [ ] 改动范围 ≤ 3 个模块 / 包
- [ ] **无** DB migration（`server/migrations/**` 等）
- [ ] **无** auth / 支付 / 权限模型变更
- [ ] **无** breaking API（或 spec 已显式允许）
- [ ] 有现成测试模式可抄
- [ ] brief 无「用户可能会喜欢…」类产品臆测

**典型例子：** 补测试、docs、小 UI 修复、i18n 补全、lint 修复、非敏感 refactor。

---

### ⚠️ `agent-assisted` — Agent 出 PR，CEO 必 merge

- 新 API 端点（非 breaking）
- 跨 4～5 个模块
- 有 migration 但已有模式可复制
- 游戏玩法微调（有 AC 但需人眼验 UX）

**流程：** 可走 Agent 流水线，但 **禁止 auto-merge**；CEO 勾 AC 后 merge。

---

### ❌ `human-only` — 禁止进 Agent 队列

- 新 API 语义 / Breaking change
- 商业模式、定价、合规定性
- 跨 5+ 包大重构
- 安全事件响应
- 无 AC 的模糊需求

**流程：** 仅人类或「Planner 只出 plan，不 Implement」。

---

## GitHub Labels 映射

| Label | 分级 |
|-------|------|
| `agent-safe` | ✅ |
| （无 label，模板默认） | ⚠️ 需人工 triage |
| `human-only` | ❌ |

运行时标签：

| Label | 含义 |
|-------|------|
| `agent-running` | 处理中 |
| `agent-blocked` | 停止，等 CEO |
| `agent-done` | PR + CI 绿 |

---

## Triage 流程（每周或每次投 backlog 时）

```text
新 Issue / brief
    → CEO 或 Autopilot「分级 bot」检查清单
    → 不满足 agent-safe → 打 human-only 或拆 ticket
    → 满足 → 打 agent-safe，进入队列
```

**拆 ticket 原则：** 一个大需求拆成多个 agent-safe 切片，每个 ≤3 模块，依赖用 Issue linking。

---

## 违规后果

违反分级 = Agent 在错误边界瞎改 → **不是 Agent 的锅，是 CEO 没分级。**

夜间 cron **只查询：**

```text
label:agent-safe -label:agent-running -label:agent-blocked
```

---

## 相关文档

- [18-definition-of-done.md](./18-definition-of-done.md)  
- [20-issue-brief-style-guide.md](./20-issue-brief-style-guide.md)  
- [02-operating-model.md](./02-operating-model.md)  
- [runbooks/blocked-triage.md](../runbooks/blocked-triage.md)  
