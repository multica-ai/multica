# 示例产品线：SaaS MVP（saas-stripe-mvp）

带 **订阅意图** 的 SaaS 站范例：**支付路径一律 human-only**，Agent 只做营销页、Dashboard 壳、非支付 API。

```bash
bash multica/scripts/ai-company/scaffold-saas.sh ../saas-stripe-mvp
bash multica/scripts/ai-company/bootstrap-project.sh ../saas-stripe-mvp \
  --repo YOUR_ORG/saas-stripe-mvp --create-repo --push \
  --sync-backlog --from TICKET-001 --to TICKET-004
```

| 文件 | 说明 |
|------|------|
| [brief.md](./brief.md) | SaaS MVP 范围 |
| [backlog.md](./backlog.md) | 仅 agent-safe 前端任务 |
| [human-only-queue.md](./human-only-queue.md) | Stripe/Auth 禁止进 Agent 队列 |
