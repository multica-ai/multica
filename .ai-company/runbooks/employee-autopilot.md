# Runbook — Employee Autopilot（让员工自己干活）

对齐刘小排「闭环默认转」+ 硅谷 Owner / DoD / 自动调度 / 升级矩阵。

## 一句话

**CEO 不喊「派单」时，白天 Autopilot 也会把可派的 `agent-safe` 票推进；人只接升级。**

## 组件

| 组件 | 路径 |
|------|------|
| 值班经理脚本 | `scripts/ai-company/autopilot-dispatch.sh` |
| 组合派单 | `scripts/ai-company/portfolio-dispatch.sh --local` |
| 状态 | `~/.multica/autopilot-state.json` |
| 日志 | `~/.multica/autopilot-logs/` |
| 工作区 cron | `install-autopilot-cron.sh --install`（Linux / 备用） |
| **macOS 推荐** | `autopilot-launchagent-service.sh install` — GUI 会话内自主派单，避免 cron 丢登录态 |
| 心跳 | `.cursor/HEARTBEAT.md` 白天可补一刀 |

## 行为规则

1. **安静时段** 23:00–06:00（Asia/Shanghai）：不自动派单（`--force` 可覆盖）
2. **可派条件**：`QUEUE(agent-safe)>0` 且本机并发未满（默认 ≤2）
3. **不盲派**：带 `agent-blocked` / `agent-running` / `agent-done` 的票由 `pick_next_issue` 跳过
4. **仅 BLOCKED**：不派单；飞书摘要升级（若已配置 notify）
5. **空转**：连续 2 次派单后 QUEUE 不降 → 飞书空转告警
6. **并发**：`AUTOPILOT_MAX_CONCURRENT` 默认 2（不提高危险并发）
7. **后台派单**：`portfolio-dispatch` 以 nohup 启动，Autopilot 不等待 Agent 跑完
8. **注意（本机 CLI）**：`--local` 路径下多票是**串行**的（一张跑完才派下一张）。`max_total=2` 表示本轮最多派 2 张，不表示两张并行。真并行需改 dispatch 模型（架构级，需批准）

## DoD / Owner

- 每张票 **Implementer** 为唯一代码 Owner
- **DoD** = `accept_cases.md` 里的可执行命令（复刻站含 `make visual-check`）
- 无 DoD / 复刻缺 inventory+wont_do → NEED_CLARIFY / BLOCKED，禁止口头完工

## 置信度分流

| 档 | 条件 | 动作 |
|----|------|------|
| Auto | Verifier/CI 绿 + merge-policy allow | PR / agent-done / 可 auto-merge |
| Human | 密钥、CF login、支付、改 workflows、BLOCKED×3 | 停 + 飞书升级 |

## 手动命令

```bash
# 只决策不派单
bash scripts/ai-company/autopilot-dispatch.sh --dry-run --force

# 立即跑一刀（忽略安静时段）
bash scripts/ai-company/autopilot-dispatch.sh --force

# 看队列
bash scripts/ai-company/ceo-dashboard.sh
```

## 相关

- **[33 — 自主迭代完整方案](../docs/33-autonomous-iteration.md)**（硅谷 · 刘小排对表 · 端到端闭环）
- [35 — 产品情报站](../docs/35-product-intel-lounge.md) · [product-intel-lounge.md](./product-intel-lounge.md)（好用版上线清单）
- [visual-replica-gate.md](./visual-replica-gate.md)
- [07-quality-gates.md](../docs/07-quality-gates.md)
- [blocked-triage.md](./blocked-triage.md)
- 系统进化周回顾：[`docs/system-evolution/`](../docs/system-evolution/)
