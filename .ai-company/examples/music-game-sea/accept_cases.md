# Acceptance Cases — music-game-sea

## Verification commands

**Verifier 最低栏（PR 前）：**

```bash
pnpm typecheck
pnpm test --filter @music-game/web
pnpm test --filter @music-game/core
make test
```

**全量（PR 描述必须贴最后一次成功输出）：**

```bash
make check
# 或分项：
pnpm lint
pnpm exec playwright test e2e/smoke-game.spec.ts
```

**契约（有 API 变更时）：**

```bash
vacuum lint api/openapi.yaml --fail-severity error
# CI 中 oasdiff breaking 由 api-contract-gate workflow 执行
```

## Functional criteria — MVP launch

### 落地页与合规

- [ ] AC-L1: `/` 200，含「Play」CTA、Privacy Policy、`/terms` 链接  
- [ ] AC-L2: Cookie 横幅首次访问出现；拒绝后仍可使用核心玩法（无非必要跟踪脚本）  
- [ ] AC-L3: `en` 为默认 locale；`/zh`、`/ja` 切换后标题与 CTA 文案变化  

### 核心玩法

- [ ] AC-G1: `/play` 可开始一局；demo 曲播放；4 lane 有音符下落  
- [ ] AC-G2: 按键/触摸判定产生 Perfect/Good/Miss 反馈（可见 UI）  
- [ ] AC-G3: 曲结束显示 score、max combo、accuracy%；可输入 nickname（≤16）提交  

### API

- [ ] AC-A1: `POST /v1/sessions/{sessionId}/scores` 合法 payload 返回 201 + score id  
- [ ] AC-A2: 畸形 payload / 超限分数返回 4xx，不写入榜  
- [ ] AC-A3: `GET /v1/leaderboards/demo-track` 返回 ≤50 条，按 score 降序  

### 非功能

- [ ] AC-N1: `pnpm typecheck` exit 0  
- [ ] AC-N2: 新增/变更 Go handler 有 `_test.go` 覆盖 happy path + 一个 4xx  
- [ ] AC-N3: Playwright `e2e/smoke-game.spec.ts` exit 0  
- [ ] AC-N4: OpenAPI lint exit 0（若本 ticket 触达 API）  

## Compliance script gates（CI 必须，见 compliance-checklist.md）

- [ ] AC-C1: `pnpm test compliance/cookie-banner.test.ts` exit 0  
- [ ] AC-C2: `pnpm test compliance/privacy-links.test.ts` exit 0  

## Evidence（Verifier 填写）

| Command | Exit code | Last run (UTC) |
|---------|-----------|----------------|
| | | |

## CEO sign-off

- [ ] 已对照 AC 勾选或抽检 CI + E2E 录屏  
- [ ] 确认本 PR 未触及 Out of Scope 路径  

签名 / 日期：
