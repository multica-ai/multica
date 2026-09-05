# 脱手运行 — 完成清单（2026-08-26）

> **一屏总览战略**：[docs/17-ceo-cockpit.md](./docs/17-ceo-cockpit.md) — 脱手靠本清单 + cron；「资产 / 规范 / 流程一目了然」靠升级 `:9477` 指挥舱，不把 Multica 改成公司 OS。

## 已自动化（无需盯盘）

| 能力 | 命令 / 证据 |
|------|-------------|
| 夜间全流程 | `ceo-nightly.sh`：reconcile → merge → reconcile → **sync backlog** → 后台派单 → 飞书日报 |
| **白天自主派单** | `autopilot-launchagent-service.sh install`（macOS GUI 会话，每 30min）→ 见 [docs/33-autonomous-iteration.md](./docs/33-autonomous-iteration.md) |
| Cron | `install-nightly-cron.sh --install` → 21:00 |
| 飞书日报 | `setup-feishu-bot-notify.sh` |
| 队列修复 | `ceo-reconcile-queue.sh`（含 PR 冲突 → BLOCKED） |
| 自动 merge | `ceo-auto-merge.sh` |
| 补票 | `sync-portfolio-backlogs.sh`（`--skip-existing`） |
| 验收 | `verify-hands-off.sh` → 应全绿 |
| 规范同步 | `sync-company-norms.sh` → 各产品 `.delivery/company-os/`（见 [docs/27-norm-sync.md](./docs/27-norm-sync.md)） |
| Multica 并发 | `multica-runtime-status.sh`、工作台 `:9477` |
| 飞书审批（可选） | `setup-feishu-approval.sh` + `CEO_FEISHU_APPROVAL_PUSH=1` + `ceo-feishu-cloudflare-tunnel.sh quick-install` |
| 飞书 inbound 最后一步 | `setup-feishu-approval-token.sh` → `feishu-approval.env` → `print-feishu-inbound-setup.sh` |

## 一次性配置（若尚未做）

```bash
bash scripts/ai-company/verify-hands-off.sh
bash scripts/ai-company/setup-feishu-bot-notify.sh
bash scripts/ai-company/install-nightly-cron.sh --install
bash scripts/ai-company/sync-company-norms.sh    # 规范副本 → 各产品 .delivery/company-os/
# 可选：飞书卡片审批
bash scripts/ai-company/setup-feishu-approval.sh --test
bash scripts/ai-company/ceo-feishu-approval-service.sh install
# 飞书开放平台 Request URL: https://<公网>/feishu/event（需内网穿透）
```

`local.env` 建议：

```bash
export CEO_NIGHTLY_DISPATCH=1
export CEO_AUTO_MERGE=1
export CEO_SYNC_BACKLOG=1
export CEO_FEISHU_APPROVAL_PUSH=1
```

## 仍需你偶尔介入的边界

- **BLOCKED** 需求澄清（或配置飞书审批卡）
- **新品线**：飞书一句话 / site-factory（Work-Finder **不**擅自开新站）
- **paused 项目**（如 `music-game-sea`）不会派单 — 取消 `paused` 才会消化
- **Mac 21:00 勿睡眠**（或 cron 跑在常开机器）

补票：`work-finder.sh` 在 QUEUE 薄时自动往已有产品 backlog 加 agent-safe 小票并 sync（见 runbooks/work-finder.md）。

## 日常只看飞书

无 BLOCKED → 不回复。有 BLOCKED → 飞书回一句或点审批卡。

工作台：http://127.0.0.1:9477  
日志：`~/.multica/ceo-nightly.log`、`~/.multica/ceo-nightly-dispatch.log`
