# Suggested Backlog — beatscape (MusicSaas)

仅 **agent-safe**；MLX/Gateway 见 MusicSaas `.delivery/beatscape/human-only-queue.md`。

### TICKET-B06 [agent-safe] Library 页 SEO title/description

- **What:** `apps/beatscape/src/pages/Library.tsx` 使用 `pageMeta` 设置 title/description  
- **AC:** `pnpm --filter @beatscape/web typecheck` exit 0；`pageMeta.test.ts` 仍绿  

### TICKET-B07 [agent-safe] Calibration 页 SEO title/description

- **What:** `apps/beatscape/src/pages/Calibration.tsx` 使用 `pageMeta`  
- **AC:** `pnpm --filter @beatscape/web typecheck` exit 0  

### TICKET-B08 [agent-safe] Settings 页 SEO title/description

- **What:** Settings 相关页面使用 `pageMeta`（title/description 与路由一致）  
- **AC:** `pnpm --filter @beatscape/web typecheck` exit 0；相关测试仍绿  

### TICKET-B09 [agent-safe] 404 页最小文案与 metadata

- **What:** BeatScape web 404 路由或 fallback 页；`pageMeta` title 含站点名  
- **AC:** `pnpm --filter @beatscape/web typecheck` exit 0  

### TICKET-B10 [agent-safe] Home 页 SEO title/description

- **What:** `apps/beatscape/src/pages/Home.tsx` 使用 `pageMeta`  
- **AC:** `pnpm --filter @beatscape/web typecheck` exit 0  

### TICKET-B11 [agent-safe] Play 页 SEO 回归测试补强

- **What:** `pageMeta.test.ts` 覆盖 `buildPlayPageMeta` 边界（无 seo block）  
- **AC:** `pnpm --filter @beatscape/web test` exit 0  
