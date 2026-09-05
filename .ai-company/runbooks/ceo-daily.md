# CEO 每日 Runbook（15 分钟）

> 经营面（一人公司 OPC）对接见 [../docs/13-opc-bridge.md](../docs/13-opc-bridge.md)。

## 自动模式（推荐）

每晚 **21:00** 本机 cron 自动：**派单 → 日报 → 飞书/Slack 推送**。

```bash
# 一次性配置
cp .ai-company/config/local.env.example .ai-company/config/local.env
# 编辑 FEISHU_WEBHOOK_URL 或 SLACK_WEBHOOK_URL

bash scripts/ai-company/install-nightly-cron.sh --install
```

详见 [nightly-ceo-brief.md](./nightly-ceo-brief.md)。**有 BLOCKED 才需要你回话**；无 BLOCKED 可睡。

手动试跑：

```bash
bash scripts/ai-company/ceo-nightly.sh --no-dispatch   # 只出日报
bash scripts/ai-company/ceo-daily-brief.sh             # 日报 + webhook
```

## 0. 打开仪表盘

**浏览器工作台（推荐）：**

```bash
bash ~/Projects/multica/scripts/ai-company/ceo-workbench.sh
# → http://127.0.0.1:9477
```

**终端一条命令：**

```bash
cd ~/Projects/multica
bash scripts/ai-company/ceo-dashboard.sh
# 有队列且无 BLOCKED 时顺带派活：
bash scripts/ai-company/ceo-dashboard.sh --dispatch
```

可选：`source .ai-company/config/local.env`（从 `local.env.example` 复制）

- Multica Issues（或各 repo GitHub）
- Slack #ai-company-alerts（若配置）
- OPC 杀线 / 本周全力：SecondBrain `03-MAPS/...-map-portfolio-opc.md`

## 1. 阻塞清零（优先级最高）

```bash
# 对每个生产项目 repo
gh issue list -l agent-blocked --json number,title,url
```

- 每条 BLOCKED → [blocked-triage.md](./blocked-triage.md)
- 目标：**上班前 BLOCKED = 0**（或已明确排期）

## 2. 昨夜交付确认

- 若启用 **Portfolio 调度**（multica 仓 `portfolio-agent-dispatch`）：看 Actions 是否对各 repo 成功 `workflow_run`
- 筛选各产品 repo 昨夜 merge 的 `cursor/*` PR
- **不读 diff**；仅确认：
  - [ ] CI 全绿
  - [ ] PR body 含 AC 勾选 + verify 输出
  - [ ] 非 deny 路径误 auto-merge（抽查 1 条即可）

## 3. 队列补给（5 分钟）

- 从 backlog 挑 1～3 条 **已分级为 agent-safe** 的 ticket
- 确保 `accept_cases.md` 可勾选、brief 无歧义
- 打 label `agent-safe`（若尚未）

## 4. 成本扫一眼

- Cursor / API 用量是否 >80% 月预算
- 是 → 今日暂停非 P0 项目 Autopilot

## 5. 结束

- 无 BLOCKED、队列有粮、预算正常 → **今天可以躺平**

---

## 不做清单

- ❌ 逐行 review Agent 代码（除非 security label）
- ❌ 在 BLOCKED 未清时投模糊需求
- ❌ 手动 merge 未绿 CI 的 PR
