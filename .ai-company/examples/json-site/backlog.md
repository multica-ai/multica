# Backlog — json-site (chenzh/json-site)

JSON 格式化站点；TICKET-001～005 已在 GitHub 关闭。续票供夜间 sync / dispatch。

### TICKET-006 [agent-safe] Privacy / Terms 静态页

- **What:** `/privacy`、`/terms` 路由或静态页，链到 footer  
- **AC:** 两页可访问；`pnpm typecheck` exit 0  

### TICKET-007 [agent-safe] favicon 与 web manifest

- **What:** `apps/web/public/favicon.ico`、`site.webmanifest`（name/theme_color）  
- **AC:** `pnpm typecheck` exit 0；manifest 路由可访问  

### TICKET-008 [agent-safe] robots.txt 与 sitemap 占位

- **What:** `apps/web/public/robots.txt`；最小 sitemap（静态或生成脚本）  
- **AC:** `make check` exit 0  

### TICKET-009 [agent-safe] Copy/Download 无障碍与 aria 标签

- **What:** 格式化结果区与 Copy/Download 按钮补充 `aria-label` / 键盘可达  
- **AC:** `pnpm test` exit 0；无新增 a11y 回归  

### TICKET-010 [agent-safe] 深色模式 toggle 占位

- **What:** header 增加 theme toggle（localStorage 占位，无完整主题系统）  
- **AC:** `pnpm typecheck` exit 0  

### TICKET-011 [agent-safe] 页脚外链与 sitemap 链

- **What:** footer 链到 `/privacy`、`/terms`、`/sitemap.xml`  
- **AC:** `make check` exit 0  

### TICKET-012 [agent-safe] 404 页与基础 metadata

- **Owner:** Implementer
- **What:** 增加最小 not-found / 404 文案与站点 title metadata
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-013 [agent-safe] OG / Twitter card meta

- **Owner:** Implementer
- **What:** 首页补充 openGraph / twitter meta 占位
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-014 [agent-safe] 静态约页性能：图片 alt

- **Owner:** Implementer
- **What:** 关键图片补有意义 alt；无图则跳过改文案区 landmark
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29
