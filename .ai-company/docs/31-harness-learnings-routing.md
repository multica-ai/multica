# 31 — Harness 经验回流路由

> **层级**：通用（Company OS）  
> **问题**：对话能读到 harness，但经验默认只进 INBOX，不会自动回到正确规范层。  
> 配套：[28-norm-layers.md](./28-norm-layers.md) · [27-norm-sync.md](./27-norm-sync.md) · [32-opc-harness-knowledge-design.md](./32-opc-harness-knowledge-design.md) · [system-evolution/harness-candidates.md](./system-evolution/harness-candidates.md)

---

## 原则

1. **聊天不是真相源** — 可检索结论才入库（见 SecondBrain `AUTO-DEPOSIT.md`）。
2. **先路由、后升格** — 写入 `harness-candidates` 待审队列；**禁止** Agent 无审批 PATCH 宪法正文或 `.cursor/rules/*.mdc`。
3. **仍用薄指针** — 升格时改 Markdown 权威文件，不把全文塞进 Cursor Rules。
4. **冲突优先级** — 任务 > 项目 brief > 项目 CLAUDE > company-os > Vault HARNESS（同 [28](./28-norm-layers.md)）。

---

## 关键词 → 目标层（路由表）

| 命中场景 / 关键词 | 层 | 权威写入目标 | 同步 / 动作 |
|-------------------|-----|--------------|-------------|
| `agent-safe` `NEED_CLARIFY` 分级争议 | 1 通用 | `docs/06-task-grading.md` | manifest → `sync-company-norms.sh` |
| `BLOCKED` `VERIFY_EXHAUSTED` `POLICY_DENY` | 1 通用 | `docs/21-label-state-machine.md` + `runbooks/blocked-triage.md` | 同上 |
| `BLOCKED:INFRA` `cursor-agent` `proxy` `pnpm install` | 1 通用 | `docs/23-local-agent-environment.md` | 同上 |
| `Verifier` `exit 0` `DoD` `quality gate` CI 门禁 | 1 通用 | `docs/18-definition-of-done.md` · `docs/07-quality-gates.md` | 同上 |
| `merge-policy` `POLICY_DENY` deny 路径 | 1+2 | `docs/06` + 项目 `.delivery/config/merge-policy.json` | norms + 产品仓 commit |
| `好票` Issue 模板 AC 歧义 | 1 通用 | `docs/20-issue-brief-style-guide.md` | sync-company-norms |
| `Tier-0` `company-harness` `省 token` `vault-harness` | 1 通用 | `docs/27-norm-sync.md` | + `rollout-harness-tier0.sh` |
| `选型` `定稿` `弃用` `不再改` `架构` | 0 Vault | `HARNESS/projects/{slug}.md` · profile | `sync-all-harness.sh` |
| Multica TS/Go 包边界 / 迁移 / API 兼容 | 产品 | 根 `CLAUDE.md` | 产品仓 commit |
| 某站禁止路径 / 栈 / 验收命令 | 2 项目 | `.delivery/<slug>/brief.md` · `accept_cases.md` | 产品仓 only |
| `OPC` `杀线` `本周主线` | 经营 | Vault OPC map · `docs/13-opc-bridge.md`（HQ） | 周回顾，不进每票 |
| 仅本票范围 | 3 任务 | GitHub Issue comment / AC | 不升格 |

**口诀**：换产品线仍成立 → 层 1；只此 repo → 层 2；只此 Issue → 层 3；编码习惯跨 OPC → Vault。

---

## Agent 动作（里程碑 / BLOCKED 解决后）

1. **判定** — 用上表选 **一层**（至多两层：通用 + 项目微调）。
2. **记录** — 运行（或等效写入）：
   ```bash
   bash scripts/ai-company/record-harness-learning.sh \
     --content "结论一句话" \
     --suggest "docs/18-definition-of-done.md"
   ```
3. **SecondBrain** — 若可验收，静默 `deposit`（`-Source milestone`）+ Rule W wiki-ingest；用户说「这条别进大脑」则跳过。
4. **禁止**在候选队列写入密钥、token、webhook URL（`record-harness-learning.sh` 会拒绝）。
5. **回复** — 一句：`已记入 harness 候选：→ <目标文件>`；**不要**声称已改宪法正文。

---

## CEO 周回顾（升格）

每周一或 `system-evolution` 周报时：

1. 读 [harness-candidates.md](./system-evolution/harness-candidates.md) 未处理项（≤15 分钟）。
2. 升格 **≤3 条** 到上表目标文件；其余保留或关闭为「仅任务级」。
3. 层 1 改动 → `sync-company-norms.sh` → `portfolio-commit-norms.sh --commit`。
4. Vault 层 → 改 SecondBrain `HARNESS/` → `sync-all-harness.sh`。
5. 在当周 `YYYY-MM-DD-weekly.md` 写「本周 harness 升格」小节。

---

## 口令

| 你说 | 动作 |
|------|------|
| `记录 harness 经验：[结论]` | `record-harness-learning.sh` + 可选 deposit |
| `整理 harness 候选` | CEO 扫 harness-candidates + 周报复盘 |
| `这条别进大脑` | 本轮禁止 deposit / 候选 |

---

## 反模式

- ❌ 把经验 append 进 `company-harness.mdc` 或 User Rules（token 膨胀）
- ❌ 只 deposit INBOX、从不 PATCH 目标 doc
- ❌ 口头「知道了」但不更新 brief / runbook（下一张票再 BLOCKED）
- ❌ 产品仓手改 `company-os/` 副本（应改 HQ + sync）

---

## 相关

- [system-evolution/README.md](./system-evolution/README.md)
- SecondBrain `10-SYSTEM/AUTO-DEPOSIT.md` · `AUTO-EVOLVE.md` Rule G
