# Competitor Inventory — <project-slug>

<!-- 复刻 / 竞品对齐的结构真相源。无此文件 → Planner NEED_CLARIFY。 -->

## Reference sources

| Role | URL | Notes |
|------|-----|-------|
| Primary | | 主参考站；若 CF 人机验证抓不到，标注原因 |
| Fallback | | 替代参考（设计/结构接近即可） |
| Captured at | YYYY-MM-DD | 截图或手工观察日期 |

## Pages / routes

| Route | Purpose | In MVP? |
|-------|---------|---------|
| `/` | | yes |
| | | |

## Components (home minimum)

| ID | Component | Visible content / behavior | Breakpoints |
|----|-----------|----------------------------|-------------|
| C-nav | Top nav / brand | | desktop, 375 |
| C-hero | Hero / title | | desktop, 375 |
| C-main | Primary interactive (filter/search/CTA) | | desktop, 375 |
| C-list | Main content list/grid | | desktop, 375 |
| C-detail | Detail / secondary panel (if any) | | desktop, 375 |

## Interaction matrix (minimum)

| ID | Action | Expected |
|----|--------|----------|
| I-load | Open `/` | 关键关键区块出现，无白屏 |
| I-nav | 主导航或筛选 | UI 状态变化可观察 |
| I-cta | 主 CTA / 打开详情 | 有反馈（面板、路由或提示） |
| I-mobile | 视口 375 宽 | 布局可用，无横向溢出 |

## Explicit non-goals

→ 详见同目录 `wont_do.md`（必须存在）。
