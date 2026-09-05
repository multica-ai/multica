# 22 — Git 与远程仓库规范

> **层级**：通用（公司）+ 项目（各产品 remote）  
> **multica HQ** 与 **产品仓** 规则不同，见下表。

---

## 仓库角色

| 仓 | 远程 | 用途 |
|----|------|------|
| **multica**（HQ） | `origin` = multica-ai/multica（只读）<br>`fork` = chenzh/multica（推送） | `.ai-company/`、harness 源、CEO 脚本 |
| **产品仓** | `origin` = chenzh/`<product>` | brief、代码、Agent PR |
| **SecondBrain** | 独立 | Vault harness，非交付代码 |

---

## multica HQ 推送（硬规则）

```bash
# ✅ 唯一推荐
bash scripts/ai-company/push-fork.sh

# ❌ 禁止（非 maintainer 403）
git push origin main
```

- `main` 应跟踪 `fork/main`（`verify-hands-off.sh` 会检查）
- Harness / 规范变更：commit multica → `push-fork.sh` → 再 `sync-company-norms.sh` 到产品

---

## 产品仓规范

| 项 | 规则 |
|----|------|
| 默认分支 | `main`（或 registry 登记一致） |
| Agent 分支 | `cursor/<ticket>-<slug>`（前缀 `cursor/`，见 merge-policy） |
| 推送 | `git push origin <branch>` — 产品仓 **推 origin**，不推 multica fork |
| PR | Agent 开 PR → `cursor/*` → `main` |
| merge 谁做 | auto-merge（policy+CI 绿）或 CEO（assisted / deny） |

---

## 本机 clone 与 path

- **不**把路径写进 `project-registry.yaml`（不进 git）
- 本机配置：
  - `.ai-company/config/local.env` → `AI_REPO_PATH_<id>=/path`
  - 或 `repo-paths.local.yaml`（gitignore）
- 解析：`scripts/ai-company/resolve-repo-path.sh --id <id>`

---

## Agent 与 Git 边界

| 允许 Agent | 禁止 Agent |
|------------|------------|
| 在 `cursor/*` 分支 commit | push 到 `main` |
| 开 PR | merge（除非 policy 明确 bot merge） |
| 读 `.delivery/*` | 改 `merge-policy` 扩大 allow（human-only） |
| | 改 `.github/workflows`（human-only） |
| | commit `.env`、密钥 |

---

## worktree / 多 checkout

- multica `make up` / worktree：见根 `CLAUDE.md` Makefile
- 产品仓：一机一 path；避免 24h 只本地 commit 不 push（见 disaster-recovery runbook）

---

## 规范变更后的 Git 顺序

```text
1. multica: 改 .ai-company/ → commit → push-fork.sh
2. sync-company-norms.sh → 各产品 .delivery/company-os/
3. 各产品: commit + git push origin
4. 若改 agents/workflows: install-harness.sh --force <product>
```

---

## 相关文档

- [27-norm-sync.md](./27-norm-sync.md)  
- [28-norm-layers.md](./28-norm-layers.md)  
- [scripts/ai-company/push-fork.sh](../../scripts/ai-company/push-fork.sh)  
