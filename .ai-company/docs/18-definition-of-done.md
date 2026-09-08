# 18 — 完成定义（Definition of Done）

> **层级**：通用（Company OS）→ 同步到 `.delivery/company-os/`  
> **项目差异**：具体命令写在项目 `CLAUDE.md` 与 `.delivery/<slug>/accept_cases.md`  
> **任务差异**：Issue AC 可收窄，不可放宽通用 DoD

---

## 原则

1. **只有 exit code 算完成** — Verifier / CI 输出为准，禁止口头「应该没问题」。
2. **DoD 必须可执行** — 每条 AC 对应命令或可查证的检查项（勾选 + 证据）。
3. **缺 DoD 不得开工** — Planner 无 `accept_cases` / Issue AC → `NEED_CLARIFY`。
4. **合并 ≠ 完成** — merge 前 DoD 已绿；CEO 勾 AC 是验收权威（可抽检）。

---

## 三级 DoD

### L0 — 任务（单张 Issue）

每张进队列的票 **至少** 具备：

| 项 | 要求 |
|----|------|
| 范围 | What & why ≤3 句；Out of scope 必填 |
| AC | Issue 勾选列表 **或** 明确引用 `accept_cases.md` 章节 |
| 分级 | 已打 `agent-safe` 且满足 [06-task-grading.md](./06-task-grading.md) |
| 证据 | PR body 含最后一次成功运行的命令输出 |

**任务级完成：** Issue AC 全绿 + PR CI 绿 + 符合 merge-policy。

### L1 — 项目（产品线）

`.delivery/<slug>/accept_cases.md` 定义本产品 **最低验收栏**：

```bash
# 示例结构（按项目裁剪）
pnpm test / make test
make check                    # 全量前
make visual-check             # 复刻/落地页必选
```

复刻/落地页 **额外** L1 要求（缺一不可）：

- [ ] `competitor_inventory.md` 非空
- [ ] `wont_do.md` 已勾选边界
- [ ] Structure / Interaction / Visual AC 见 [templates/accept_cases.md](../templates/accept_cases.md)

**项目级完成：** 某次发布或里程碑前，L1 全表勾选 + CEO sign-off（可写在 AC 底部）。

### L2 — 公司（门禁层）

见 [07-quality-gates.md](./07-quality-gates.md)：

- L1 Verifier 环 → L2 PR CI → L3 契约 → L4 E2E / Visual → L5 merge-policy

**公司级完成：** 任一层 exit ≠ 0 → **未交付**，不得 merge（human-only 路径除外）。

---

## Verifier 环（Agent 必做）

1. 按 `accept_cases.md` + Issue AC **逐条**执行命令。
2. 在 PR body 粘贴 **完整** 最后一次成功输出（含 exit code）。
3. exit ≠ 0 → 交还 Implementer，**最多 3 轮**。
4. 第 3 轮仍失败 → `agent-blocked`，告警 CEO。

禁止：不跑命令写 PASSED；只跑子集却宣称全量通过。

---

## PR body 最低模板

```markdown
## Issue
- Closes #<N> / .delivery/<slug>/

## Acceptance
- [ ] AC-1: … (command: `…` exit 0)
- [ ] …

## Verification evidence
\`\`\`
<paste last successful run>
\`\`\`

## Risks / deferred
- …
```

---

## 与分级的关系

| 分级 | DoD 终点 |
|------|----------|
| `agent-safe` | Verifier 绿 + CI 绿 + merge-policy 允许 → 可 auto-merge |
| `agent-assisted` | 同上，但 **CEO 必 merge**（勾 AC） |
| `human-only` | 不进 Agent 环；人工 DoD 自定 |

---

## 常见不合格（直接 BLOCKED）

- AC 只有「页面好看」「功能完整」无可执行命令
- 复刻票无 inventory / wont_do 仍开写代码
- `make visual-check` 未跑却声称视觉通过
- PR 无验证输出
- 改动落在 merge-policy **deny** 路径

---

## 相关文档

- [06-task-grading.md](./06-task-grading.md)  
- [07-quality-gates.md](./07-quality-gates.md)  
- [20-issue-brief-style-guide.md](./20-issue-brief-style-guide.md)  
- [28-norm-layers.md](./28-norm-layers.md)  
