# Runbook — Visual Replica Gate（网站复刻完整度）

把「像不像竞品」从 vibe 变成可失败门禁。

## 何时启用

- Site Factory 新建站
- 竞品复刻 / 营销落地页 ticket
- `accept_cases.md` 含 Visual 或 `make visual-check`

## 必须产物

| 文件 | 作用 |
|------|------|
| `.delivery/<slug>/competitor_inventory.md` | 路由、组件、交互矩阵 |
| `.delivery/<slug>/wont_do.md` | 显式不做 |
| `.delivery/<slug>/accept_cases.md` | Structure + Interaction + Visual AC |
| `e2e/visual.spec.ts` + `*-snapshots/` | Playwright 基线（入库） |

无 inventory / wont_do → Planner **NEED_CLARIFY**，Verifier **BLOCKED**。

## 本地命令

```bash
pnpm install
# CI：pnpm exec playwright install --with-deps chromium
# 本机若 ms-playwright 下载被代理/证书拦截：默认用系统 Chrome（PW_CHANNEL=chrome）
pnpm exec playwright install chromium   # 可选
# 首次或故意更新基线：
pnpm exec playwright test --grep @visual --update-snapshots
# 门禁（Verifier 跑这个）：
make visual-check
```

默认容差：`maxDiffPixelRatio = 0.02`（见 `playwright.config.ts`）。  
本机默认 `channel: chrome`（`PW_CHANNEL` 可覆盖）；CI workflow 安装 Playwright Chromium。

## Verifier 行为

1. 读 accept_cases + inventory + wont_do  
2. 跑 `make check`（若适用）+ **`make visual-check`**  
3. exit ≠ 0 → **BLOCKED**（最多 3 轮 fix）  
4. **禁止**不跑命令写「看起来完整」

## 冒烟（证明门禁会红）

1. 基线绿  
2. 故意改 H1 / 品牌字号或文案  
3. `make visual-check` **必须失败**  
4. 还原后再绿  

## 明确不做

- 不追求整站 1:1 像素  
- 不绕竞品 Cloudflare 人机验证  
- 不强制 Chromatic / Percy  

## 相关

- [07-quality-gates.md](../docs/07-quality-gates.md)（L4-视觉）  
- [15-feishu-site-factory.md](../docs/15-feishu-site-factory.md)  
- `.cursor/agents/verifier.md`
