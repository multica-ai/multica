# Backlog — cloudflare-site

> 每票：**Owner=Implementer**；**DoD**=下方 AC 对应的 `accept_cases.md` 命令（含 `make visual-check` 若适用）。

### TICKET-001 [agent-safe] Vite 空壳与 harness 验证

- **Owner:** Implementer
- **What:** `apps/web` + `pnpm typecheck` + vitest 占位 + `wrangler.toml`
- **DoD:** `make check` exit 0

### TICKET-002 [agent-safe] 落地页 SEO metadata

- **Owner:** Implementer
- **What:** `index.html` / root layout title、description、OG tags
- **DoD:** accept_cases metadata AC + `make check`

### TICKET-003 [agent-safe] 核心工具或落地页 UI

- **Owner:** Implementer
- **What:** 按 brief 实现主功能（textarea + 交互或 hero + CTA）
- **DoD:** AC-1/AC-2 + `make visual-check`

### TICKET-004 [agent-safe] Privacy / Terms 静态页

- **Owner:** Implementer
- **DoD:** AC-3 + `make check`

### TICKET-005 [agent-safe] Cloudflare Pages 构建与 wrangler 校验

- **Owner:** Implementer
- **What:** `pnpm build` 产出 `apps/web/dist`；CI 跑 typecheck + test + visual
- **DoD:** AC-4 + `make check` + `make visual-check`
