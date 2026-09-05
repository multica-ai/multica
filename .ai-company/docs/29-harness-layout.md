# 29 — Harness 与文档布局（按类型）

> **权威索引**：什么放哪、什么会同步、什么只是种子。  
> 配套：[28-norm-layers.md](./28-norm-layers.md)（分层）· [27-norm-sync.md](./27-norm-sync.md)（同步命令）  
> 更新：2026-08-29

---

## 一句话

| 问题 | 答案 |
|------|------|
| 公司规范写在哪？ | **multica** `.ai-company/`（权威） |
| 产品仓读什么？ | `.delivery/company-os/`（只读副本）+ 本仓 `CLAUDE.md` + `.delivery/<slug>/` |
| 案例放哪？ | **HQ** `.ai-company/examples/<slug>/`（种子，**不同步**） |
| 执行脚手架？ | `.ai-company/harness/` → `install-harness.sh` / `install-content-harness.sh` |
| multica 本仓代码规范？ | 根目录 **`CLAUDE.md`** / **`AGENTS.md`**（与 Company OS 分开） |

---

## 类型总表（`.ai-company/`）

| 类型 | 目录 | 用途 | 进 git | 同步到产品 `company-os` |
|------|------|------|:------:|:----------------------:|
| **编号规范** | `docs/NN-*.md` | 公司宪法、门禁、分层、接线 | ✅ | **manifest 子集**（见下） |
| **Runbook** | `runbooks/*.md` | CEO 日/周/夜间、接入、BLOCKED | ✅ | manifest 子集 |
| **空模板** | `templates/*` | brief、AC、CLAUDE 骨架、kickoff | ✅ | manifest 子集 |
| **种子案例** | `examples/<slug>/` | 已填好的 brief/backlog **范例** | ✅ | ❌ **永不** |
| **Harness** | `harness/` | 安装到目标仓的脚手架源 | ✅ | ❌（经 install 复制 workflows/agents） |
| **HQ 配置** | `config/*` | registry 模板、defaults、manifest | ✅（`.example`） | ❌ |
| **HQ 密钥** | `config/*.env` 等 | 飞书、代理、本机路径 | ❌ gitignore | ❌ |
| **台账模板** | `templates/project-registry.yaml` | 调度真相（HQ 编辑） | ✅ | ❌ |
| **脱手清单** | `HANDS-OFF-COMPLETE.md` | 验收条目 | ✅ | ✅ |
| **系统进化** | `docs/system-evolution/` | 周报（HQ 维护） | ✅ | ❌ |

**口诀：** `docs` + `runbooks` + `templates` 里 **编了号的规范** 可下发；`examples/` 只用来 **复制**，不是运行时真相。

---

## `docs/` 编号与是否下发

### 下发到产品（在 [company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml)）

| 编号 | 文件 | 主题 |
|------|------|------|
| 01 | vision | 愿景 |
| 02 | operating-model | 运转模型 |
| 06 | task-grading | agent-safe 分级 |
| 07 | quality-gates | CI / Verifier |
| 17 | ceo-cockpit | 指挥舱 |
| 18 | definition-of-done | DoD |
| 19 | asset-registry | 台账字段 |
| 20 | issue-brief-style-guide | 好票 |
| 21 | label-state-machine | Label / BLOCKED |
| 22 | git-and-remotes | fork 规则 |
| 23 | local-agent-environment | 本机 cursor 环境 |
| 24 | content-operations | 自媒体线 |
| 27 | norm-sync | 同步管道 |
| 28 | norm-layers | 通用 vs 项目 vs 任务 |
| 29 | harness-layout | **本文** |
| 30 | silicon-valley-doc-standards | 硅谷文档纪律（conventions 对齐） |
| 31 | harness-learnings-routing | 经验回流路由 + 候选队列 |

另：`HANDS-OFF-COMPLETE.md`、`runbooks/*`（3）、`templates/*`（3）— 见 manifest 全文。

### 仅 HQ 阅读（不下发）

| 编号 | 文件 | 原因 |
|------|------|------|
| 00 | getting-started | CEO 上手周计划 |
| 03 | organization | 编制（HQ） |
| 04 | architecture | 全公司架构图 |
| 05 | stack-selection | 选型 |
| 08 | multi-project-portfolio | portfolio 细节 + registry |
| 09 | compliance-and-risk | 合规长文 |
| 10 | cost-and-budget | 预算 |
| 11 | langgraph-when-and-how | P2 编排 |
| 12 | ceo-dashboard | 仪表盘（CLI） |
| 13 | implementation-roadmap | P0→P3 |
| 13 | opc-bridge | OPC 桥接 |
| 14 | multica-autopilot-portfolio | Autopilot |
| 15 | feishu-site-factory | 建站工厂 |
| 16 | disaster-recovery | 灾备摘要 |
| 32 | opc-harness-knowledge-design | **设计方案总览**（战略/arch，不下发） |
| 33 | autonomous-iteration | **自主迭代完整方案**（硅谷 · 刘小排对表） |
| 35 | product-intel-lounge | **产品情报站**（好用版 · 飞书卡片 + 口令开票） |

---

## `examples/` — 案例（种子包）

**角色：** 给 CEO / onboard 时 **复制** 到产品仓 `.delivery/<slug>/` 的**已填范例**，不是任何环境的运行时真相。

| 子目录 | 产品线类型 | 复制目标 |
|--------|------------|----------|
| [music-game-sea](../examples/music-game-sea/) | 出海游戏 + API + 合规 | `.delivery/music-game-sea/` |
| [landing-tool-a](../examples/landing-tool-a/) | 静态落地页 | `.delivery/landing-tool-a/` |
| [saas-stripe-mvp](../examples/saas-stripe-mvp/) | SaaS + 支付 human-only | `.delivery/saas-stripe-mvp/` |
| [json-site](../examples/json-site/) | Cloudflare 工具站 | `.delivery/json-site/` |
| [cloudflare-site](../examples/cloudflare-site/) | site-factory 示范 | 新建站时参考 |
| [beatscape](../examples/beatscape/) | 音乐 SaaS 子产品 | `.delivery/beatscape/` |

**每个 example 包典型文件：**

| 文件 | 层 | 说明 |
|------|-----|------|
| `README.md` | — | 如何复制、接入命令 |
| `brief.md` | 2 项目 | 范围、禁止路径 |
| `accept_cases.md` | 2 项目 | 验收命令 |
| `backlog.md` | 2 项目 | 建议票 → `sync-backlog-to-issues.sh` |
| `competitor_inventory.md` | 2 项目 | Visual Replica 用 |
| `wont_do.md` | 2 项目 | Visual Replica 用 |
| `api_spec.openapi.yaml` | 2 项目 | 有 API 时 |
| `human-only-queue.md` | 2 项目 | 支付等 |

```bash
# 标准复制（接入新产品）
cp -r multica/.ai-company/examples/<slug> /path/to/product/.delivery/<slug>
cp multica/.ai-company/templates/CLAUDE.project.md /path/to/product/CLAUDE.md
# 填写 CLAUDE.md 后 install harness
bash multica/scripts/ai-company/install-harness.sh /path/to/product
```

**反模式：** 在 `examples/` 改完就当产品真相 — 必须复制到产品仓再改。

索引：[examples/README.md](../examples/README.md)

---

## `harness/` — 执行脚手架（按产品线类型）

| 路径 | 类型 | 安装命令 | 装进目标仓 |
|------|------|----------|------------|
| [harness/install.sh](../harness/install.sh) | **工程 / 产品** | `install-harness.sh` | `.cursor/agents/`、`.github/workflows/agent-delivery-*`、`scripts/agent-delivery/`、`.delivery/_template` |
| [harness/install-content-harness.sh](../harness/install-content-harness.sh) | **内容 / 自媒体** | `install-content-harness.sh` | `scripts/content-delivery/`、`content-delivery-dispatch.yml`、`CONTENT-HQ-SPLIT.md` |
| [harness/scaffold/](../harness/scaffold/) | 工程 GHA 源文件 | （install 复制） | workflows、issue 模板 |
| [harness/scaffold-content/](../harness/scaffold-content/) | 内容 GHA 源文件 | （install 复制） | 同上 |
| [harness/content-hq-split.md](../harness/content-hq-split.md) | 职责划分 | content install | `.delivery/CONTENT-HQ-SPLIT.md` |

工程 kickoff 模板：[templates/orchestrator-kickoff-product.md](../templates/orchestrator-kickoff-product.md)  
内容 kickoff 模板：[templates/orchestrator-kickoff-content.md](../templates/orchestrator-kickoff-content.md)

---

## `templates/` — 空骨架（无业务内容）

| 文件 | 用途 | 落点 |
|------|------|------|
| `CLAUDE.project.md` | 产品仓根 `CLAUDE.md` | 产品仓根 |
| `project-brief.md` | brief 空壳 | `.delivery/<slug>/brief.md` |
| `accept_cases.md` | AC 空壳 | `.delivery/<slug>/` |
| `competitor_inventory.md` / `wont_do.md` | 复刻站 | `.delivery/<slug>/` |
| `merge-policy.json` | merge 白名单 | `.delivery/config/` |
| `orchestrator-kickoff-*.md` | 派单 prompt | `.delivery/prompts/` |
| `project-registry.yaml` | 调度台账 | **仅 HQ** `.ai-company/templates/` |
| `site-factory/*` | 飞书建站 prompt | HQ 脚本读 |

---

## `config/` — HQ 本机

| 文件 | 进 git | 说明 |
|------|:------:|------|
| `company-os-sync-manifest.yaml` | ✅ | 下发列表权威 |
| `company-defaults.yaml` | ✅ | 公司默认（cron、agent 列表） |
| `*.example` / `*.yaml.example` | ✅ | 复制后本地填 |
| `local.env` / `proxy.env` / `feishu-*.env` | ❌ | CEO 机器密钥 |
| `repo-paths.local.yaml` | ❌ | 产品 checkout 路径 |
| `company-assets.local.yaml` | ❌ | 域名/CF 登记 |

---

## multica 本仓库：两套规范并存

multica 既是 **Company OS 权威源**，也是 **Multica 产品代码仓**。

```text
multica/   （本仓库）
├── CLAUDE.md              ← 层「产品代码」：TS/Go 包边界、测试、迁移硬规则
├── AGENTS.md              ← Agent 入口指针（→ CLAUDE.md）
├── .delivery/             ← 层 2「本仓交付实例」：dogfood 流水线说明、merge-policy
│   └── README.md          ← Sleep Mode 架构（Multica 自身）
├── .ai-company/           ← 层 1「公司 OS」权威（本文档所在树）
│   ├── docs/              ← 编号规范
│   ├── examples/          ← 种子（给其他产品复制）
│   ├── harness/           ← 安装脚手架
│   ├── runbooks/          ← CEO 程序
│   ├── templates/         ← 空模板
│   └── config/            ← manifest、defaults
├── scripts/ai-company/    ← HQ 自动化（不复制到产品仓）
├── apps/ server/ packages/← Multica 产品实现（受 CLAUDE.md 约束）
└── 产品仓副本（各 fork）   ← .delivery/company-os/ + CLAUDE.md
```

| 我要改… | 编辑 | 不要编辑 |
|---------|------|----------|
| Go/TS 代码规则 | `CLAUDE.md` | `.ai-company/docs/` |
| 全公司 DoD / 好票 | `.ai-company/docs/18`… | 产品仓 `company-os`（应 sync） |
| Multica 自己的 agent 交付 | `.delivery/` + 根 workflows | `examples/` |
| 新产品 brief | `examples/<slug>` 复制 → 产品仓 | 只改 example 不复制 |
| 夜间派单台账 | `templates/project-registry.yaml` | — |

**推送规则：** multica HQ 只 push **`fork`**；各 **产品仓** push `origin`（见 [22-git-and-remotes.md](./22-git-and-remotes.md)）。

---

## 三条同步管道（勿混）

| 管道 | 权威 | 命令 | 落地 |
|------|------|------|------|
| SecondBrain Harness | Vault `HARNESS/` | `sync-all-harness.sh` | `.cursor/rules/vault-harness.mdc`、`docs/VAULT-HARNESS.md` |
| company-harness | `.ai-company/harness/` | `install-harness.sh [--force]` | 目标仓 `.delivery/`、agents、workflows |
| Company OS 规范 | `.ai-company/docs/` + manifest | `sync-company-norms.sh` | 目标仓 `.delivery/company-os/` |

---

## 新增规范时的检查单

1. **判定层级** — [28-norm-layers.md](./28-norm-layers.md) 通用 / 项目 / 任务？
2. **选目录** — 通用 → `docs/` 或 `runbooks/`；项目 → `examples/` 种子或产品仓；任务 → Issue 模板
3. **是否下发** — 通用且 Agent 必读 → 追加 [manifest](../config/company-os-sync-manifest.yaml)
4. **同步** — `sync-company-norms.sh` → `portfolio-commit-norms.sh --commit --push`
5. **Harness** — 若改 workflow/agent → `install-harness.sh --force` 各产品
6. **指挥舱** — 新 doc 链到 [17-ceo-cockpit.md](./17-ceo-cockpit.md) 规范区（可选）

---

## 相关

- [harness/HARNESS-INDEX.md](../harness/HARNESS-INDEX.md) — 本页快捷入口（harness 目录内）
- [examples/README.md](../examples/README.md) — 案例包索引
- [README.md](../README.md) — Company OS 总目录
