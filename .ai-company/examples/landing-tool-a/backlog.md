# Backlog — landing-tool-a

### TICKET-001 [agent-safe] Next 空壳与 harness 验证

- **What:** `apps/web` + `pnpm typecheck` + vitest 占位  
- **AC:** `pnpm typecheck` exit 0  

### TICKET-002 [agent-safe] 落地页 SEO metadata

- **What:** `layout.tsx` title/description、OG tags  
- **AC:** AC-1 相关 metadata 存在  

### TICKET-003 [agent-safe] JSON 格式化工具 UI

- **What:** textarea + format 按钮 + 错误提示  
- **AC:** AC-1、AC-2  

### TICKET-004 [agent-safe] Privacy / Terms 静态页

- **AC:** AC-3  

### TICKET-005 [agent-safe] favicon 与 web manifest

- **What:** `apps/web/public/favicon.ico`、`site.webmanifest`（name/theme_color）  
- **AC:** `pnpm typecheck` exit 0；`/manifest.webmanifest` 或等价路由可访问  

### TICKET-006 [agent-safe] robots.txt 与 sitemap 占位

- **What:** `apps/web/public/robots.txt`；`app/sitemap.ts` 返回最小 sitemap  
- **AC:** `pnpm typecheck` exit 0  

### TICKET-007 [agent-safe] 404 页与 metadata

- **What:** `app/not-found.tsx` 最小文案；`metadata` title 为站点名  
- **AC:** `pnpm typecheck` exit 0  

### TICKET-008 [agent-safe] JSON minify 模式切换

- **What:** 工具页增加 format / minify 切换；minify 输出紧凑 JSON  
- **AC:** AC-1、AC-2；`pnpm typecheck` exit 0  

### TICKET-009 [agent-safe] 键盘快捷键 Format

- **What:** textarea 聚焦时 Cmd/Ctrl+Enter 触发 format  
- **AC:** 单测或组件测试覆盖快捷键；`pnpm test` exit 0  

### TICKET-010 [agent-safe] Footer 站点链接与版权行

- **What:** 全局 footer 链到 `/privacy`、`/terms`；版权年份  
- **AC:** `pnpm typecheck` exit 0  

### TICKET-011 [agent-safe] 补齐验收：AC-1: `/` 渲染工具输入框与格式化按钮

- **Owner:** Implementer
- **What:** 实现或加固未勾选验收项：AC-1: `/` 渲染工具输入框与格式化按钮
- **AC / DoD:** 对应 accept_cases 项可验证；`make check` 或项目默认 check exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-012 [agent-safe] OG tags 补齐

- **Owner:** Implementer
- **What:** layout metadata 增加 openGraph 字段
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-013 [agent-safe] OG tags 补齐

- **Owner:** Implementer
- **What:** layout metadata 增加 openGraph 字段
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-014 [agent-safe] 页脚合规链接

- **Owner:** Implementer
- **What:** footer 链到 Privacy/Terms
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-015 [agent-safe] OG tags 补齐

- **Owner:** Implementer
- **What:** layout metadata 增加 openGraph 字段
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-016 [agent-safe] 页脚合规链接

- **Owner:** Implementer
- **What:** footer 链到 Privacy/Terms
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-017 [agent-safe] OG tags 补齐

- **Owner:** Implementer
- **What:** layout metadata 增加 openGraph 字段
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-018 [agent-safe] 页脚合规链接

- **Owner:** Implementer
- **What:** footer 链到 Privacy/Terms
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29
