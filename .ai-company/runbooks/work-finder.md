# Runbook — Work-Finder（找活工）

对齐「不用 CEO 往 backlog 手工加票」：队列薄时自动造 `agent-safe` 小票 → sync Issue → Autopilot 消化。

## 一句话

**Autopilot 负责派；Work-Finder 负责造票；CEO 只批新品线与 BLOCKED。**

## 组件

| 组件 | 路径 |
|------|------|
| 找活脚本 | `scripts/ai-company/work-finder.sh` |
| 启发式造票 | `scripts/ai-company/lib/work-finder-heuristic.py` |
| Agent 提示 | `.ai-company/templates/work-finder-prompt.md` |
| 状态 | `~/.multica/work-finder-state.json` |
| 日志 | `~/.multica/work-finder-logs/` |
| cron | 飞书工作区 `cron-jobs.json`：周末每 2h；工作日 08:00 |

## 行为规则

1. **安静时段** 23:00–06:00：不找活（`--force` 可覆盖）
2. **只在 QUEUE 薄时造票**：`TOTAL_QUEUE < QUEUE_TARGET`（默认 3）
3. **每轮上限**：公司合计默认 ≤3；单项目 ≤2
4. **只动已有产品**：读 `project-registry` 非 paused + 已有 `examples/<slug>/backlog.md`
5. **禁止**：新开站、改 merge-policy、密钥、支付真连接、`human-only` 票
6. **造票后**：`sync-portfolio-backlogs.sh --skip-existing`；派单交给 Autopilot
7. **模式**：默认 `heuristic`（cron 稳）；`--mode agent` / `auto` 可让 cursor-agent 想票（失败回退启发式）

## 手动命令

```bash
# 只看会造什么
bash scripts/ai-company/work-finder.sh --dry-run --mode heuristic --force

# 立刻找活并 sync
bash scripts/ai-company/work-finder.sh --force --mode heuristic

# Agent 想票（更「自己想」，更慢）
bash scripts/ai-company/work-finder.sh --force --mode agent
```

## 与 Autopilot 分工

| 角色 | 触发 | 产出 |
|------|------|------|
| Work-Finder | QUEUE&lt;目标 | backlog + GitHub issues |
| Autopilot | QUEUE&gt;0 且空闲 | 本地派单 |

## 相关

- [employee-autopilot.md](./employee-autopilot.md)
- [disaster-recovery.md](./disaster-recovery.md)
- [15-feishu-site-factory.md](../docs/15-feishu-site-factory.md)（新品线仍走 CEO 一句话）
