# 事件响应 Runbook

## P0 — 立即（15 分钟内）

| 事件 | 动作 |
|------|------|
| 密钥进 repo / 日志 | 轮换密钥；revoke；`git filter-repo` 如已 push；暂停所有 Autopilot |
| 恶意依赖 / RCE 告警 | 锁定 merge；human-only；隔离 runtime |
| 生产数据泄露怀疑 | 停 Agent；保全日志；法务路径 |

```bash
# 暂停 Autopilot（Multica CLI 示例）
multica autopilot pause <id>

# 禁用 dispatch workflow（GitHub UI 或 push 禁用 commit）
```

---

## P1 — 当日

| 事件 | 动作 |
|------|------|
| Agent PR CI 连续红 | 查是否 harness 漂移；暂停该项目队列 |
| Cursor API 配额耗尽 | 降并发；CEO 升配额或等重置 |
| 合规脚本失败上线 | 回滚 deploy；修脚本再发 |

---

## P2 — 本周

| 事件 | 动作 |
|------|------|
| 单项目 BLOCKED 堆积 | blocked-triage + 拆 ticket |
| 成本超 100% | 全公司暂停夜间队列至下月 |

---

## 事后

1. Issue/postmortem：根因、.harness 补丁、是否修 merge-policy  
2. 更新 [09-compliance-and-risk.md](../docs/09-compliance-and-risk.md) 如适用  
3. 恢复 Autopilot 前跑 **trivial agent-safe** 试跑  

---

## 联系清单（填你的实际信息）

| 角色 | 联系 |
|------|------|
| CEO | |
| 基础设施 | |
| 法务/合规 | |
