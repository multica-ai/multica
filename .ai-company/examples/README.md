# Examples — 产品线种子包索引

> **不是运行时真相。** 每个子目录是已填好的 **范例**，接入时复制到产品仓 `.delivery/<slug>/`。  
> 权威说明：[docs/29-harness-layout.md](../docs/29-harness-layout.md)

---

## 何时用 examples vs templates

| 场景 | 用 |
|------|-----|
| 全新产品线，需要参考完整 brief/backlog | `examples/<slug>/` **复制** |
| 只要空骨架自己填 | `templates/project-brief.md` 等 |
| 已在产品仓运行 | **只改产品仓** `.delivery/<slug>/`，回头可选更新 examples 作种子 |

---

## 种子包一览

| 目录 | 对应 registry id | 栈 | 典型文件 |
|------|------------------|-----|----------|
| [music-game-sea](./music-game-sea/) | （示例，可自建仓） | Next + Go + PG | brief, AC, OpenAPI, compliance, backlog |
| [landing-tool-a](./landing-tool-a/) | `landing-tool-a` | Next 静态 | brief, AC, backlog |
| [saas-stripe-mvp](./saas-stripe-mvp/) | `saas-stripe-mvp` | Next + Go SaaS | brief, AC, human-only-queue, backlog |
| [json-site](./json-site/) | `json-site` | Cloudflare Pages | brief, backlog |
| [cloudflare-site](./cloudflare-site/) | site-factory 示范 | CF Pages | brief, backlog, accept_cases |
| [beatscape](./beatscape/) | `beatscape` | Vite / 子应用 | brief, backlog |
| [meigen-replica](./meigen-replica/) | `meigen-replica` | Visual replica (static + Playwright) | brief, AC, backlog, inventory, wont_do |

---

## 标准接入（复制种子）

```bash
SLUG=landing-tool-a
PRODUCT=/path/to/$SLUG

cp -r multica/.ai-company/examples/$SLUG $PRODUCT/.delivery/$SLUG
cp multica/.ai-company/templates/CLAUDE.project.md $PRODUCT/CLAUDE.md
# 编辑 $PRODUCT/CLAUDE.md

bash multica/scripts/ai-company/install-harness.sh $PRODUCT
bash multica/scripts/ai-company/sync-company-norms.sh --id $SLUG
```

内容线（自媒体）不用 `examples/` 工程包 — 见 [docs/24-content-operations.md](../docs/24-content-operations.md) + `install-content-harness.sh`。

---

## 包内文件约定

| 文件 | 必填 | 说明 |
|------|:----:|------|
| `README.md` | 推荐 | 本包说明与接入命令 |
| `brief.md` | ✅ | 层 2 产品宪法 |
| `accept_cases.md` | ✅ | Verifier 命令 |
| `backlog.md` | 推荐 | → `sync-backlog-to-issues.sh` |
| `competitor_inventory.md` | 复刻站 | Visual Replica |
| `wont_do.md` | 复刻站 | Visual Replica |
| `api_spec.openapi.yaml` | 有 API 时 | 契约门禁 |
| `human-only-queue.md` | 支付等 | human-only 票说明 |

---

## 反模式

- ❌ 直接改 `examples/` 指望已上线产品自动更新  
- ❌ 把 `examples/` 全文 sync 进 `company-os/`  
- ❌ 在 example 里写 CEO 本机路径或密钥
