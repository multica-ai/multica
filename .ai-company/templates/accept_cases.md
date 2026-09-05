# Acceptance Cases — <project-slug>

<!-- Verifier 按此执行；exit code 必须 0。CEO 交付前勾选。禁止口头宣称「很像」。 -->

## Prerequisites (replica / landing)

- [ ] `.delivery/<slug>/competitor_inventory.md` 存在且组件表非空
- [ ] `.delivery/<slug>/wont_do.md` 存在且勾选边界
- 缺失任一 → **NEED_CLARIFY** / **BLOCKED**（不要开写代码）

## Verification commands

最低栏（按项目裁剪；复刻站必须含视觉命令）：

```bash
pnpm test --filter <package>   # 或 vitest / make test
make check
# 复刻 / 落地页视觉门禁（必跑）：
make visual-check
# 等价：pnpm exec playwright test --grep @visual
```

## Structure（对照 inventory）

- [ ] AC-S1: inventory 中标记 In MVP 的路由可访问
- [ ] AC-S2: 组件表 C-* 在页面上可定位（角色/文案/data-testid）
- [ ] AC-S3: wont_do 未实现项未偷偷做进 MVP

## Interaction（对照 interaction matrix）

- [ ] AC-I1: I-load — `/` 加载无白屏
- [ ] AC-I2: I-nav 或主筛选 — 状态可观察
- [ ] AC-I3: I-cta — 主 CTA/详情有反馈
- [ ] AC-I4: I-mobile — 375 宽可用

## Visual（Playwright）

- [ ] AC-V1: `home-desktop` 截图对比通过（`maxDiffPixelRatio` ≤ 0.02）
- [ ] AC-V2: `home-mobile-375` 截图对比通过
- [ ] AC-V3: 故意改坏关键标题后 `make visual-check` **必须失败**（冒烟时验证一次即可）

## Functional（项目自有）

- [ ] AC-1: 
- [ ] AC-2: 

## Non-functional（若适用）

- [ ] 无新增 lint / type 错误
- [ ] OpenAPI lint（若有 API）
- [ ] E2E smoke 非视觉路径（若有）

## Evidence（Verifier 填写）

| Command | Exit code | Last run (UTC) |
|---------|-----------|----------------|
| | | |

## CEO sign-off

- [ ] 我已对照 inventory + 截图抽检（不必读全 diff）

签名 / 日期：
