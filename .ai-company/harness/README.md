# company-harness — 可复制交付脚手架

把任意 git 仓库变成 AI 公司流水线节点：**一条命令复制 harness**，再填 `brief.md` 即可进队列。

## 包含什么

安装到目标项目后会有：

```text
<target-repo>/
  .delivery/              # 交付真相源 + merge-policy + orchestrator prompt
  .cursor/agents/         # planner · implementer · verifier · reviewer · orchestrator
  .cursor/rules/
    company-harness.mdc   # Tier-0 pointer (alwaysApply; ~20 lines — not full company-os)
  .github/workflows/
    agent-delivery-gate.yml
    api-contract-gate.yml   # 有 OpenAPI 时启用
  scripts/agent-delivery/   # dispatch-cli · build-prompt · check-merge-eligible
```

公司级文档仍放在 **Multica 仓** `.ai-company/`（或你 fork 的 `company-os` 仓），不随 harness 全文复制。

**规范副本** 下发到各产品仓：

```bash
bash scripts/ai-company/sync-company-norms.sh
```

见 [../docs/27-norm-sync.md](../docs/27-norm-sync.md)。

---

## Cursor Tier-0（省 token，全对话遵循）

薄指针规则在 `.cursor/rules/`（`alwaysApply: true`），**禁止**把 company-os / Vault 全文写进规则或 User Rules。

| 仓类型 | 规则 |
|--------|------|
| multica HQ | `vault-harness` · `zbrain-session` · `company-harness` · `code-index` |
| 产品仓（有 `.delivery`） | `vault-harness` · `zbrain-session` · `company-harness` |

```bash
# 本机一键 rollout + 验收
bash scripts/ai-company/rollout-harness-tier0.sh
bash scripts/ai-company/verify-harness-tier0.sh
```

见 `.ai-company/docs/27-norm-sync.md` § Cursor 省 token。

**经验回流**：`docs/31-harness-learnings-routing.md` · `record-harness-learning.sh` · `verify-harness-learnings.sh`

---

## 安装（在 multica 仓库内）

```bash
# 安装到当前目录（默认）
bash .ai-company/harness/install.sh

# 安装到另一个项目
bash .ai-company/harness/install.sh /path/to/your-game-site

# 仅预览
bash .ai-company/harness/install.sh --dry-run /path/to/target
```

或从仓库根：

```bash
bash scripts/ai-company/install-harness.sh ../my-landing-page
```

---

## 安装后必做（CEO，约 30 分钟）

1. Labels：`agent-safe` `agent-running` `agent-blocked` `agent-done`
2. CEO 本机：`cursor-agent login`（派单只用 session，无 API key）
3. （可选）GitHub Secret：`SLACK_WEBHOOK_URL`
4. `main` branch protection：required checks 与 workflow job 名一致
5. 复制示例或模板到 `.delivery/<slug>/`（如 [../examples/music-game-sea/](../examples/music-game-sea/)）
6. 按 [../runbooks/onboard-new-project.md](../runbooks/onboard-new-project.md) 试跑 trivial ticket

---

## 独立 repo 化（≥3 个项目时推荐）

```bash
# 从 multica 抽出 harness 为独立仓库（一次性）
cp -r .ai-company/harness /tmp/company-harness
cp -r .delivery/_template /tmp/company-harness/scaffold/.delivery/_template
# … 或使用 install.sh 的 SOURCE_ROOT 指向 multica clone

git init company-harness && cd company-harness
# 各新项目：git submodule add <company-harness> .harness
# CI 中调用 .harness/install.sh
```

---

## 自定义

| 文件 | 何时改 |
|------|--------|
| `.delivery/config/merge-policy.json` | 每项目风险不同 |
| `.github/workflows/api-contract-gate.yml` | 改 OpenAPI 路径 |
| `.cursor/agents/*.md` | 全公司统一；慎改 |

---

## 内容线 harness（自媒体 / 远程 Hermes）

```bash
bash .ai-company/harness/install-content-harness.sh /path/to/content-repo
# 或
bash scripts/ai-company/install-content-harness.sh /path/to/content-repo
```

安装后内容仓会有 **`.delivery/CONTENT-HQ-SPLIT.md`** — Harness 权威职责表（司令部 vs 内容工位）。

| 文档 | 用途 |
|------|------|
| [content-hq-split.md](./content-hq-split.md) | Harness 源文件；`install-content-harness` 复制到内容仓 |
| [../docs/24-content-operations.md](../docs/24-content-operations.md) | 接线、远程机、token、落地清单 |
| `.delivery/prompts/orchestrator-kickoff.md` | Hermes worker 每次派单读取 |

**分工摘要：** CEO 本机 = Issues / `ceo-nightly` / 工程 cursor / 飞书 / 发布；远程 lighthouse = `pull-dispatch` + Hermes oneshot + `drafts/` PR。勿双派单（Kanban vs Issues）。

---

## 相关

- [../README.md](../README.md) — AI 公司 OS 总索引  
- [../examples/music-game-sea/](../examples/music-game-sea/) — 首个产品线示例包  
