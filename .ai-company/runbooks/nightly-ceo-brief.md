# 每晚 CEO 简报（21:00）

## 做什么

1. **reconcile**：修复僵死 `agent-*` 标签（有 open PR 标 `agent-done`，无 PR 回 `agent-safe`）
2. **auto-merge**：合并 CI 全绿的 agent PR，并清理关联 issue 标签
3. **派单**（后台）：`portfolio-dispatch --local`，不阻塞日报；日志见 `~/.multica/ceo-nightly-dispatch.log`
4. **日报 + 飞书**：汇总 BLOCKED / 交付 / **指挥舱链接与资产摘要** / Multica 并发 / 建议动作

飞书摘要首行：`📊 指挥舱 http://127.0.0.1:9477`（可用 `CEO_WORKBENCH_URL` 覆盖）

```bash
bash scripts/ai-company/verify-hands-off.sh   # 脱手验收
bash scripts/ai-company/company-overview-snapshot.sh   # 仅 JSON 快照（日报用）
bash scripts/ai-company/ceo-nightly.sh --sync-dispatch  # 若要等派单完成再收日报
```

## 一次性配置

```bash
cp .ai-company/config/local.env.example .ai-company/config/local.env
cp .ai-company/config/proxy.env.example .ai-company/config/proxy.env   # 国内访问 GitHub

# 飞书 webhook（一次性）
bash scripts/ai-company/setup-feishu-notify.sh 'https://open.feishu.cn/open-apis/bot/v2/hook/...'

# 试跑（不派单）
bash scripts/ai-company/ceo-daily-brief.sh --no-notify

# 试跑（派单 + 日报）
bash scripts/ai-company/ceo-nightly.sh

# 安装 cron（每晚 21:00，本机时区）
bash scripts/ai-company/install-nightly-cron.sh --install
```

确保 Mac 未睡眠，或 cron 跑在常开机器上。

## CEO 只需回三种话

| 情况 | 你回 |
|------|------|
| BLOCKED 澄清 | 「选 A / 补充一句需求」 |
| 绿 PR 待 merge | 「merge #xx」或依赖 auto-merge |
| 无 BLOCKED | 不回复，继续睡 |

想细看资产/规范：打开日报里的 **指挥舱** 链接，或 `bash scripts/ai-company/ceo-workbench.sh`

## 日志

- 日报 markdown：`~/.multica/ceo-briefs/brief-*.md`
- nightly 日志：`~/.multica/ceo-nightly.log`
- 后台派单：`~/.multica/ceo-nightly-dispatch.log`
