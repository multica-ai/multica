# Human-only queue — saas-stripe-mvp

**永不打 `agent-safe` label。**

| ID | 标题 | 原因 |
|----|------|------|
| PAY-001 | Stripe Checkout + Customer Portal | 支付 |
| PAY-002 | Webhook `checkout.session.completed` | 支付 + 密钥 |
| AUTH-001 | OAuth (Google/GitHub) | 身份 |
| AUTH-002 | Session/JWT 中间件 | 权限模型 |
| MIG-001 | 首条 users/subscriptions migration | DB |

CEO 或资深工程师手动实现；完成后由 Agent 接 **非支付** 任务。
