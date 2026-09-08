# Project Brief — cloudflare-site

## Meta

| 字段 | 值 |
|------|-----|
| Project ID | `cloudflare-site` |
| Tier | experiment |
| 栈 | **Cloudflare Pages + Workers（Wrangler）** — 无 Vercel / 无自建 VPS |

## What & Why

1. 海外用户可用的 **单页工具或营销站**（示例：JSON 格式化器）— SEO 友好、加载快。  
2. 前端部署 **Cloudflare Pages**；如需轻量 API 用 **Workers**（Hono），数据用 **D1/KV** 仅在 brief 显式要求时启用。  
3. 首版默认 **无登录、无支付**。

## In Scope

- `/` 工具 UI 或落地页 + 简短 SEO 文案
- `/privacy` `/terms` 静态页
- `wrangler.toml` + Pages 构建流水线（CI）
- 基础 i18n（en 默认）

## Out of Scope

- 非 Cloudflare 托管（Vercel、Netlify、Docker 生产部署）
- 用户账号 / Stripe / 数据库迁移（除非 brief 显式允许）
- 竞品调研结论以外的功能扩张

## Technical Notes

- 前端：`apps/web`（Vite + React + TypeScript）
- 部署：`wrangler pages deploy` 或 GitHub → Cloudflare Pages 连接
- 质量：`pnpm typecheck`、`pnpm test`、`make check` exit 0

## Acceptance

见 [accept_cases.md](./accept_cases.md)。
