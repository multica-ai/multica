# Suggested Backlog — music-game-sea

按依赖顺序排列。复制为 GitHub Issue，打 `agent-safe`（除非标注）。

---

## Sprint 0 — Harness 与冒烟（P0）

### TICKET-001 [agent-safe] 项目骨架与 harness

- **What:** Next.js + Go monorepo 空壳，`make check` 空通过或最小测试  
- **AC:** `pnpm typecheck` + `make test` exit 0  
- **Out of scope:** 游戏逻辑  
- **Note:** 基线已在 `../music-game-sea` 预脚手架；Agent 任务改为「验证并补齐缺口」

### TICKET-002 [agent-safe] 安装 compliance 测试占位

- **What:** `compliance/cookie-banner.test.ts` 等占位（可先 skip 待实现）  
- **AC:** `pnpm test compliance` 可运行  

### TICKET-003 [agent-safe] 复制 OpenAPI 骨架

- **What:** `api/openapi.yaml` 来自 `api_spec.openapi.yaml`；CI api-contract-gate 启用  
- **AC:** vacuum lint exit 0  

---

## Sprint 1 — 落地页（agent-safe）

### TICKET-004 [agent-safe] 营销落地页 `/`

- **AC:** AC-L1、AC-L3（en only 可先）  

### TICKET-005 [agent-safe] Privacy + Terms 静态页

- **AC:** AC-L1、AC-C2  

### TICKET-006 [agent-safe] Cookie 横幅组件

- **AC:** AC-L2、AC-C1  

### TICKET-007 [agent-safe] i18n zh/ja 关键文案

- **AC:** AC-L3  

---

## Sprint 2 — 核心玩法（agent-assisted 建议人工勾 AC）

### TICKET-008 [agent-assisted] `/play` 场景与 note 渲染

- **AC:** AC-G1  
- **Note:** 判定窗口在 plan.md 定稿后实现  

### TICKET-009 [agent-assisted] 判定与结算 UI

- **AC:** AC-G2、AC-G3（仅本地分，暂不提交 API）  

### TICKET-010 [agent-assisted] Playwright smoke-game

- **AC:** AC-N3  

---

## Sprint 3 — API + 排行榜（agent-assisted）

### TICKET-011 [agent-assisted] Session + Score API

- **AC:** AC-A1、AC-A2、AC-N2  

### TICKET-012 [agent-assisted] Leaderboard API + 前端展示

- **AC:** AC-A3  

---

## 禁止进队列（human-only 示例）

- PAY-001 接入 Stripe  
- AUTH-001 用户账号体系  
- MIG-001 首条 production migration 策略  

---

## 夜间 cron 建议

`project-registry.yaml` 中本项目 `max_nightly_tickets: 2`，优先 TICKET-001～007。
