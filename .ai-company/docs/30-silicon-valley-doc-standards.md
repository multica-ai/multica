# 30 — 文档规范（硅谷对齐）

> **层级**：通用（全公司写作与仓库纪律）  
> 配套：[20-issue-brief-style-guide.md](./20-issue-brief-style-guide.md) · [29-harness-layout.md](./29-harness-layout.md)  
> 产品代码命名/i18n：**[apps/docs/developers/conventions.mdx](../../apps/docs/content/docs/developers/conventions.mdx)**（Multica 权威）

---

## 一句话

**硅谷标准 = 单一真相源 + Issue 可验证 + 英文代码面 + Runbook 可执行 + 不口头传递。**

本仓 AI 公司文档 **可以中文**（CEO / OPC），但 **仓库里进 git 的技术产物** 遵守与 Multica 产品相同的硅谷工程纪律。

---

## 与 Multica `conventions` 对齐（硬规则）

以下规则 **与产品仓相同**，见 [conventions.mdx](../../apps/docs/content/docs/developers/conventions.mdx)：

| 领域 | 规则 |
|------|------|
| **代码注释** | **English only** |
| **Commit** | Conventional：`feat(scope)` / `fix(scope)` / `chore(scope)` … |
| **API / DB** | `snake_case` on wire；TS 边界 `camelCase` |
| **路由** | 单词或 `/{noun}/{verb}`；无根路径连字符 |
| **Issue key** | 人类可读前缀 + 序号（产品内） |
| **i18n** | 遵循 glossary；`issue`→任务，`skill` 保持英文等 |

**产品仓 `CLAUDE.md` 必须链到** `.delivery/company-os/` 与本仓 `brief.md`，不得复制全文宪法。

---

## 硅谷文档类型 ↔ 本仓落点

| SV 实践 | 本仓类型 | 权威位置 | 语言 |
|---------|----------|----------|------|
| **Company handbook / OS** | 编号 `docs/` | `.ai-company/docs/` → sync `company-os/` | 中文为主，术语保留英文 |
| **Runbook / Playbook** | `runbooks/` | `.ai-company/runbooks/` | 中文 + 命令原文 |
| **RFC / ADR**（大改） | 架构 doc | `docs/04`、`11`；大改先 Issue 讨论 | 中文 |
| **PRD / Brief** | 项目层 2 | `.delivery/<slug>/brief.md` | 中文可，AC 命令英文 |
| **Spec / Ticket** | Issue | GitHub Issue body | 标题可中文；**AC 用命令** |
| **DoD** | 通用 + 项目 | `18` + `accept_cases.md` | 命令英文 |
| **README** | 入口 | 各仓根、`examples/*/README` | 中文说明 + 英文命令块 |
| **Seed / Example** | 案例 | `.ai-company/examples/` | 不 sync；复制后改产品仓 |
| **Harness** | 脚手架 | `.ai-company/harness/` | 英文脚本注释 |

---

## Issue = 可执行 Spec（对齐 Linear / 大厂 eng）

与 [20-issue-brief-style-guide.md](./20-issue-brief-style-guide.md) 一致，强调硅谷 ticket 纪律：

1. **One owner** — Implementer；CEO 不 micromanage diff  
2. **Testable AC** — 每条 = 命令或可数工件，禁止「looks good」  
3. **Out of scope** — 必填，防 scope creep  
4. **Small batch** — 一夜 CI 可证；过大拆票  
5. **Link truth** — `brief.md` / `accept_cases.md` / OpenAPI，不重复粘贴  

标题推荐：`[Agent]: <verb> <object>`（与 20 一致）。

---

## 仓库纪律（Silicon Valley monorepo hygiene）

| 规则 | 说明 |
|------|------|
| **SSOT** | 同一规则只写一处；别处链接（见 [28-norm-layers.md](./28-norm-layers.md)） |
| **No chat truth** | 决策落 Issue / doc / PR comment |
| **Manifest driven** | 通用规范变更 → `company-os-sync-manifest.yaml` → `sync-company-norms.sh` |
| **Fork discipline** | multica HQ → `fork` only；产品 → `origin`（[22](./22-git-and-remotes.md)） |
| **No secrets in git** | `config/*.env` gitignore；只登记 Secret **名**（[19](./19-asset-registry.md)） |
| **English code surface** | 变量/类型/注释/提交信息英文；CEO 文档可中文 |

---

## 语言分工（避免混用失控）

| 受众 | 文档 | 语言 |
|------|------|------|
| CEO / OPC | runbooks、`17` 指挥舱、`00` 上手 | 简体中文 |
| Agent / Verifier | Issue AC、`accept_cases`、CI 日志 | **命令与路径英文** |
| 全球协作 / 开源面 | `conventions.mdx`、API、代码 | **English** |
| 产品 UI 文案 | `packages/views/locales` | 遵循 glossary |

---

## 新文档检查单（投 manifest 前）

- [ ] 层级正确（[28](./28-norm-layers.md)：通用 / 项目 / 任务）  
- [ ] 不重复 `brief` 或 `examples` 已有 AC  
- [ ] 可执行处给出 **完整命令**（可复制）  
- [ ] 链到权威而非复制 `conventions` 全文  
- [ ] 若 Agent 必读 → 加入 [manifest](../config/company-os-sync-manifest.yaml)  
- [ ] `sync-company-norms.sh` + 产品仓 commit  

---

## 反模式（硅谷团队会拒的）

- ❌ 「口头说过了」不写 Issue  
- ❌ 中文注释进 `server/` / `packages/`  
- ❌ 产品仓自建 `.ai-company/docs/` 全文副本  
- ❌ AC 写「测试通过」无命令  
- ❌ 在 `examples/` 改完不复制到产品仓就当上线  
- ❌ 多个调度真相（Kanban + Issues 双派单）

---

## 相关

- [29-harness-layout.md](./29-harness-layout.md) — 放哪  
- [27-norm-sync.md](./27-norm-sync.md) — 怎么同步  
- [18-definition-of-done.md](./18-definition-of-done.md) — DoD  
- Multica [conventions.mdx](../../apps/docs/content/docs/developers/conventions.mdx)
