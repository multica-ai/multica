# Backlog — saas-stripe-mvp (agent-safe only)

### TICKET-001 [agent-safe] Monorepo 壳 + merge-policy 含 payment deny

- **AC:** `make check` exit 0；`merge-policy.json` deny `**/payment/**`

### TICKET-002 [agent-safe] 营销页 + Pricing 静态三栏

- **AC:** AC-1  

### TICKET-003 [agent-safe] Dashboard 布局壳

- **AC:** AC-2  

### TICKET-004 [agent-safe] GET /v1/me mock handler

- **AC:** AC-3、AC-N2（Go test）  

### TICKET-005 [agent-safe] GET /health 存活探针

- **What:** `server` 增加 `GET /health` 返回 `{"status":"ok"}`  
- **AC:** Go test 覆盖 handler；`make check` exit 0  

### TICKET-006 [agent-safe] GET /v1/workspaces mock 列表

- **What:** mock handler 返回单条 workspace JSON  
- **AC:** Go test；`make check` exit 0  

### TICKET-007 [agent-safe] OpenAPI health 路径文档

- **What:** `api/openapi.yaml` 增加 `/health` 路径与示例响应  
- **AC:** `make check` exit 0  

### TICKET-008 [agent-safe] Settings 页布局壳

- **What:** Dashboard 内 `/settings` 路由；profile 表单占位（无真实 API）  
- **AC:** AC-2；`make check` exit 0  

### TICKET-009 [agent-safe] OpenAPI /v1/me 路径文档

- **What:** `api/openapi.yaml` 增加 `/v1/me` 与 mock 响应 schema  
- **AC:** `make check` exit 0  

### TICKET-010 [agent-safe] 营销页 CTA 链到 Pricing

- **What:** 首页 hero CTA 指向 `/pricing`；Pricing 页返回首页链接  
- **AC:** AC-1；`pnpm typecheck` exit 0  

### TICKET-011 [agent-safe] OpenAPI /v1/workspaces 路径文档

- **What:** `api/openapi.yaml` 增加 `/v1/workspaces` mock 列表 schema  
- **AC:** `make check` exit 0  

### TICKET-012 [agent-safe] Dashboard 侧栏导航高亮

- **What:** 当前路由在 dashboard 侧栏有 active 样式  
- **AC:** `pnpm typecheck` exit 0  

### TICKET-013 [agent-safe] Pricing 页三栏 feature 列表

- **What:** Pricing 静态页每栏增加 3 条 feature bullet  
- **AC:** AC-1；`pnpm typecheck` exit 0  

### TICKET-014 [agent-safe] OpenAPI /v1/workspaces 示例响应测试

- **What:** Go test 校验 mock `/v1/workspaces` 响应与 openapi.yaml 一致  
- **AC:** `make check` exit 0  

### TICKET-015 [agent-safe] 补齐验收：AC-1: `/` 含 Pricing section（三档价格 **展示**，按钮无真实支付）

- **Owner:** Implementer
- **What:** 实现或加固未勾选验收项：AC-1: `/` 含 Pricing section（三档价格 **展示**，按钮无真实支付）
- **AC / DoD:** 对应 accept_cases 项可验证；`make check` 或项目默认 check exit 0
- **Source:** work-finder heuristic 2026-08-29

### TICKET-016 [agent-safe] Dashboard 空态文案

- **Owner:** Implementer
- **What:** dashboard 无数据时展示友好空态（无真实 API）
- **AC / DoD:** `pnpm typecheck` exit 0
- **Source:** work-finder heuristic 2026-08-29
