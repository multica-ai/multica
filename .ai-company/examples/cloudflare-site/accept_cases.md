# Acceptance Cases — cloudflare-site

## Prerequisites

- [ ] `competitor_inventory.md` present
- [ ] `wont_do.md` present

## Verification commands

```bash
pnpm typecheck
pnpm test
pnpm --filter @cloudflare-site/web build
make check
make visual-check
```

## Structure

- [ ] AC-S1: `/` `/privacy` `/terms` 可访问（或等价静态页）
- [ ] AC-S2: C-nav / C-hero / C-main 可见

## Interaction

- [ ] AC-I1: I-load — 首页无白屏
- [ ] AC-I2: I-cta — 核心控件或 CTA 有反馈
- [ ] AC-I3: I-mobile — 375 宽可用

## Visual

- [ ] AC-V1: `make visual-check` 通过（@visual 桌面 + 375）

## Functional

- [ ] AC-1: `/` 渲染 brief 定义的核心 UI
- [ ] AC-2: 非法输入或边界有可读反馈，不白屏
- [ ] AC-3: `apps/web/dist` 构建成功，Wrangler/Pages 与 brief 一致

## CEO sign-off

- [ ] AC 已勾选或抽检通过
