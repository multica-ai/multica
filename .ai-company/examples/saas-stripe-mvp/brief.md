# Project Brief — saas-stripe-mvp

## Meta

| 字段 | 值 |
|------|-----|
| Project ID | `saas-stripe-mvp` |
| Tier | staging |
| 栈 | Next.js + Go API |

## What & Why

1. B2B **SaaS 落地页** + 登录后 **Dashboard 壳**（无真实计费）。  
2. 验证 AI 公司在「有支付敏感区」项目上的 **.harness 隔离** 能力。  
3. Stripe Checkout、Webhook、定价页 **全部由 human-only ticket 交付**。

## In Scope（Agent）

- 营销页 `/`、定价 UI **静态展示**（按钮 disabled 或链到 `#`）
- `/dashboard` 空壳布局（sidebar + placeholder）
- `GET /health`、`GET /v1/me` mock（固定 JSON，无真实 auth）

## Out of Scope（human-only — 见 human-only-queue.md）

- `**/payment/**`、`**/billing/**`、Stripe SDK  
- 真实 OAuth / session / JWT  
- `server/migrations/**`

## Acceptance

见 [accept_cases.md](./accept_cases.md)。
