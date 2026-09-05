# 16 — 灾备策略（摘要）

公司级灾备真相源见 runbook：

→ **[runbooks/disaster-recovery.md](../runbooks/disaster-recovery.md)**

## 一句话

**GitHub 备份「能重建公司的说明书 + 代码」；密钥永不进明文仓。**

## 分层

```text
A  产品代码     → 各仓 GitHub remote（跟 PR / 日 push）
B  公司 OS      → chenzh/multica 的 .ai-company + scripts/ai-company
C  密钥         → 1Password / age 加密包（本机 + 离线）
D  公网入口     → Cloudflare named tunnel（减少 URL 漂移）
E  第二执行面   → 云主机 daemon（P1，本机挂了仍能派单）
```

## CEO 最小义务

- 批准架构级灾备投入（第二执行面、named tunnel）  
- 自己能打开密钥保险库  
- 不要求 Agent 把 `.env` 推上 GitHub  
