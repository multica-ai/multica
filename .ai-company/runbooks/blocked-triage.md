# BLOCKED 分拣 Runbook

`agent-blocked` = Agent 已停止消耗配额，**等待 CEO 输入**。

---

## 分类决策树

```text
BLOCKED
 ├─ NEED_CLARIFY（需求歧义）
 │    → CEO 编号回答 → 更新 brief/Issue → 去 blocked 标签 → 重新 agent-safe
 ├─ VERIFY_EXHAUSTED（3 轮 verify 失败）
 │    → 读 Verifier 日志
 │    ├─ 环境问题（CI 缺 secret）→ 修 infra → 重跑
 │    ├─ ticket 过大 → 拆小 → 新 Issue
 │    └─ 实现错误且难自动修 → 改 human-only 或人工 patch
 ├─ POLICY_DENY（试图改 deny 路径）
 │    → 拆 ticket 或升级 human-only
 └─ INFRA（API/配额/网络）
      → 修配置 → 重 dispatch
```

---

## CEO 回复模板（贴 Issue comment）

```markdown
## CEO 澄清 @agent

1. <问题1> → <明确答案>
2. <问题2> → <明确答案>

已更新：`.delivery/<slug>/brief.md`（或本 comment 为准）

请从 Planner 阶段重新执行。Verifier 仍须 exit 0。
```

去标签：

```bash
gh issue edit <N> --remove-label agent-blocked --add-label agent-safe
```

---

## 禁止

- ❌ 口头「差不多就行」让 Agent 继续
- ❌ 不更新 brief 就重跑（会再次 BLOCKED）
- ❌ 绕过 Verifier 直接 merge

---

## SLA

| 优先级 | CEO 响应 |
|--------|----------|
| 生产 P0 | 4h 内 |
| 一般 agent-safe | 24h 内 |
| experiment | 72h 内 |
