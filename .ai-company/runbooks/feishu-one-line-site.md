# Runbook — 飞书一句话建站

## 目标

CEO 在飞书发一句「做一个 XX 网站」，系统自动完成竞品调研、MVP 定义、Cloudflare 项目脚手架、Issue 队列与 Agent 派单；CEO 只处理 BLOCKED 与 AC 勾选。

## 一次性 setup（约 15 分钟）

1. **飞书 Bot** — `feishu-cursor-claw` 常驻  
   ```bash
   bash ~/Projects/feishu-cursor-claw/service.sh install
   ```
2. **CEO 通知** — Bot 私聊或群 webhook  
   ```bash
   bash scripts/ai-company/setup-feishu-bot-notify.sh
   ```
   **CEO 工作台**（飞书 intake 优先走此 API，自托管 autostart 会一并拉起 `:9477`）  
   ```bash
   bash scripts/ai-company/ceo-workbench.sh
   # http://127.0.0.1:9477
   ```
3. **Agent 登录**  
   ```bash
   cursor-agent login && cursor-agent status
   ```
4. **Multica 自托管**（API + daemon + 工作台 `:9477`，飞书 intake 依赖）  
   ```bash
   bash scripts/local-selfhost-autostart.sh
   # 或手动: bash scripts/ai-company/ceo-workbench.sh
   ```

## 日常用法

1. 飞书发送：`做一个 <产品想法> 网站`
2. 等待 Bot 回复「建站流水线已在后台启动」
3. 收 CEO 简报（流水线完成摘要）
4. 打开工作台或 `ceo-dashboard.sh` 看队列 / BLOCKED
5. 绿 PR → 勾 AC → merge

## 故障排查

| 现象 | 处理 |
|------|------|
| Bot 无反应 | `tail -f /tmp/feishu-cursor.log`；确认长连接在线 |
| 调研/ MVP 空文件 | 看 `.ai-company/run-logs/site-factory-*.log`；确认 cursor-agent 已登录 |
| 无 GitHub Issues | 加 `--create-repo` 或手动 `bootstrap-project.sh --sync-backlog` |
| 派单未起 | `multica-runtime-status.sh` 看并发；降 `max_nightly_tickets` |

## 手动重跑某阶段

```bash
bash scripts/ai-company/site-factory.sh \
  --slug my-site --idea "JSON formatter" \
  --skip-research --skip-mvp \
  --target ~/Projects/my-site \
  --create-repo --notify
```

详见 [15-feishu-site-factory.md](../docs/15-feishu-site-factory.md)。
