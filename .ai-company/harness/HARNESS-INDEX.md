# Harness index

> **完整布局（按类型）→ [docs/29-harness-layout.md](../docs/29-harness-layout.md)**  
> **硅谷文档纪律 → [docs/30-silicon-valley-doc-standards.md](../docs/30-silicon-valley-doc-standards.md)**
> 分层规则 → [docs/28-norm-layers.md](../docs/28-norm-layers.md) · 同步 → [docs/27-norm-sync.md](../docs/27-norm-sync.md)

## 安装入口

| 产品线 | 脚本 |
|--------|------|
| 工程 / 网站 / SaaS | [install.sh](./install.sh) → `scripts/ai-company/install-harness.sh` |
| 自媒体 / 内容 | [install-content-harness.sh](./install-content-harness.sh) |

## 目录

| 子目录 / 文件 | 类型 |
|---------------|------|
| `scaffold/` | 工程 GHA + issue 模板源 |
| `scaffold-content/` | 内容 GHA + issue 模板源 |
| `content-hq-split.md` | HQ vs 远程职责（→ 内容仓 `.delivery/CONTENT-HQ-SPLIT.md`） |
| [../examples/](../examples/) | 种子案例（**不同步**，复制用） |
| [../templates/](../templates/) | 空骨架 |
| [../docs/](../docs/) | 编号规范 |
| [../config/company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml) | 下发 manifest |

## multica 本仓

- **产品代码规范** → 仓库根 `CLAUDE.md`
- **公司 OS 权威** → `../`（`.ai-company/`）
- **本仓 dogfood 交付** → `../../.delivery/`
