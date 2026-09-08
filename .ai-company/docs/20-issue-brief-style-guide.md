# 20 — Issue / Brief 写作规范

> **层级**：通用（写法规则）+ 任务（每张 Issue 实例）  
> **模板**：[templates/project-brief.md](../templates/project-brief.md)、Issue `agent_safe_task.yml`

---

## 好票 vs 烂票

| | ✅ 好票（可 agent-safe） | ❌ 烂票（必 BLOCKED 或 human-only） |
|---|--------------------------|--------------------------------------|
| 标题 | `[Agent]: 补 landing 首页 i18n 缺失文案` | `优化一下网站` |
| 范围 | 明确文件/模块（≤3 包） | 「整个站」「所有页面」 |
| AC | `pnpm test --filter @x` exit 0 | 「测试通过」「看起来对」 |
| Out of scope | 列出禁止碰的路径 | 空白或「无」 |
| 产品臆测 | 无 | 「用户可能会喜欢…」 |
| 依赖 | 单票可独立完成 | 需先做完另 5 张票 |

---

## 标题格式（推荐强制）

```text
[Agent]: <动词> <对象> [<可选上下文>]
```

示例：

- `[Agent]: 修复 beatscape 播放列表空状态文案`
- `[Agent]: 为 json-site 添加 footer 链接（desktop）`

**禁止：** 无 `[Agent]` 前缀的模糊标题进夜间队列（需 triage 补全）。

---

## What & why（≤3 句）

1. **做什么**（可观察行为）
2. **为什么**（链到 brief 或指标，一句）
3. **边界**（一句，重复 out of scope 要点）

反例：「做一个类似竞品但更现代的网站。」

正例：「在 `/tools/json` 增加复制按钮；brief §In Scope；不改 API。」

---

## Acceptance Criteria 写法

### 必须满足

- 每条 AC **可验证**（命令、URL、DOM、截图对比）
- 含 **验证命令** 或引用 `accept_cases.md` 具体条目
- 使用 Issue **勾选列表** `- [ ]`

### 命令 AC 模板

```markdown
- [ ] `pnpm test --filter @scope/pkg` exit 0
- [ ] `make visual-check` exit 0（复刻票必选）
- [ ] 手动：改坏标题后 visual-check **必须失败**（一次性冒烟）
```

### 禁止用语

| 禁止 | 替代 |
|------|------|
| 尽量 / 差不多 / 完整复刻 | inventory 组件 ID + visual AC |
| 应该没问题 | 粘贴命令输出 |
| 参考竞品感觉 | `competitor_inventory.md` C-03 |
| 用户可能会喜欢 | 删掉或 human-only |

---

## Out of scope（必填）

至少列 3 类（可复制）：

```markdown
- No DB migrations
- No auth / payment / secrets
- No changes under .github/workflows/
- No paths outside: packages/views/landing/**
```

与 [06-task-grading.md](./06-task-grading.md) 对齐；brief 里已有则 Issue 可写「同 brief §Out of Scope」。

---

## 单票粒度

| 维度 | agent-safe 上限 |
|------|-----------------|
| 模块/包 | ≤3 |
| 预估 diff | 可一夜 review 完（CEO 不读 code，靠 CI） |
| 迁移 | 0（除非 brief 显式允许且有模式可复制 → assisted） |
| 新 API 语义 | assisted 或 human-only |

**过大 → 拆票**，用 Issue linking（`blocked by` / `parent`）。

---

## brief.md vs Issue

| 载体 | 用途 |
|------|------|
| `brief.md` | 产品背景、长期 In/Out of Scope |
| Issue | **本票** AC + out of scope |
| `backlog.md` | 种子；sync 到 Issue 后 **以 Issue 为准** |

投队列前：backlog 行 → Issue 必须满足本规范。

---

## Triage 检查清单（CEO / 派单前 30 秒）

- [ ] 已打 `agent-safe`（或故意 human-only）
- [ ] AC 含可执行命令或链到 accept_cases
- [ ] Out of scope 非空
- [ ] 不满足 [06](./06-task-grading.md) 任一条 → 降级或拆票
- [ ] 复刻票：inventory + wont_do 已存在

---

## 相关文档

- [18-definition-of-done.md](./18-definition-of-done.md)  
- [06-task-grading.md](./06-task-grading.md)  
- [21-label-state-machine.md](./21-label-state-machine.md)  
- [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) — 硅谷对齐总纲  
