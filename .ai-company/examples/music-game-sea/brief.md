# Project Brief — music-game-sea

## Meta

| 字段 | 值 |
|------|-----|
| Project ID | `music-game-sea` |
| Repo | `github.com/<your-org>/music-game-sea`（待创建） |
| Tier | production |
| Owner (CEO) | @you |
| 默认语言 | en-US；次要 zh-CN、ja-JP |

## What & Why（3 句以内）

1. 面向海外用户的 **Web 音乐节奏小游戏**：浏览器即玩，无需下载。  
2. 核心循环：选曲 → 4 轨音符下落 → 判定 Perfect/Good/Miss → 结算分数与连击。  
3. 首版目标：可公开试玩、可分享成绩链接、满足基础 GDPR 与 Cookie 同意，**不含真实货币支付**。

## Users & Context

- **目标用户：** 18+ 休闲玩家（欧美、日韩优先）；移动端浏览器为主。  
- **地区：** 全球 CDN；暂不做中国大陆专属合规包。  
- **关键约束：** GDPR Cookie 同意、隐私政策页、无 PII 强收集；首版无账号体系（匿名 session id）。

## In Scope（MVP）

- 营销落地页（玩法介绍、试玩 CTA、隐私/ToS 链接）
- 游戏场景：1 首内置 demo 曲、4 lane、基础判定与结算 UI
- 分数提交 API（匿名 session，服务端校验分数合理性上限）
- 排行榜 API（Top 50，仅 nickname + score + 时间）
- i18n：en 完整，zh/ja 关键 UI 文案
- Playwright：首页 → 开局 → 完成一局 → 看见分数

## Out of Scope（禁止 Agent 修改除非新 brief）

- `**/payment/**`、Stripe、内购  
- 用户注册/登录/OAuth  
- `server/migrations/**`（MVP 用固定 schema 或 seed，migration 由 human-only ticket 引入）  
- `**/auth/**`  
- 实时多人对战  
- 曲库版权采购与 UGC 上传  

## Technical Notes

- **前端：** `apps/web/` Next.js App Router，`packages/ui` 设计 token，禁硬编码 Tailwind 色值  
- **后端：** `server/` Go + Chi；分数逻辑服务端可测  
- **契约：** `api/openapi.yaml`（与 [api_spec.openapi.yaml](./api_spec.openapi.yaml) 同步）  
- **合规：** 见 [compliance-checklist.md](./compliance-checklist.md)  
- **参考节奏：** 判定窗口、note 数据结构需在 `plan.md` 中先定再编码  

## Acceptance

见 [accept_cases.md](./accept_cases.md)。CEO 在 merge 前勾选。

## Open Questions

- [x] 首版曲库：仅 1 首 royalty-free demo（CEO 已提供 asset 路径 `assets/audio/demo-track.mp3`）  
- [x] 排行榜：全匿名，nickname 最长 16 字符，服务端 profanity filter（简单 denylist）  
- [ ] 是否第二 sprint 加 Share to X — **human-only 产品决策，不进首版队列**
