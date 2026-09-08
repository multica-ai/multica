# 03 — 虚拟组织架构

## 编制总览

```text
                    ┌─────────────┐
                    │  CEO（你）   │
                    └──────┬──────┘
                           │ brief / AC / BLOCKED
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
    ┌────────────┐  ┌────────────┐  ┌────────────┐
    │ 调度中枢    │  │ 质量法庭    │  │ 项目台账    │
    │ Multica +  │  │ CI + 契约   │  │ registry   │
    │ GHA/LG     │  │ 脚本        │  │            │
    └─────┬──────┘  └──────┬─────┘  └────────────┘
          │                │
    ┌─────┴────────────────┴─────┐
    │         Worker 池（无调度权）  │
    ├──────────┬──────────┬────────┤
    │ Cursor×N │ Hermes×1 │OpenClaw│
    │ 编码主力  │ 安全评审  │ 情报   │
    └──────────┴──────────┴────────┘
```

---

## 角色定义

### CEO（人类）

- 输入：brief、预算、红线、AC 勾选。
- 输出：合并决策（非白名单路径）、BLOCKED 答复、产品线排序。
- **禁止**：直接改代码（除非 emergency）；绕过 CI。

### Orchestrator（编排层 — 代码，非 LLM）

- 实现：GitHub Actions、LangGraph、或 `dispatch-*.sh`。
- 职责：拉队列、派 Worker、读退出码分支、重试上限、HITL 中断。
- **唯一拥有流程决策权**。

### Planner（子 Agent — Cursor）

- 读 brief + 代码库；输出 `plan.md`、补全 `accept_cases.md`。
- 歧义 → `NEED_CLARIFY`，停止编码。

### Implementer（子 Agent — Cursor）

- 按 plan 实现；单模块迭代；不擅自改 API/迁移（除非 brief 允许）。

### Verifier（子 Agent — Cursor）

- **只跑命令**；贴 exit code 与输出；`≠0` → BLOCKED。
- 禁止声称通过而未执行。

### Reviewer（子 Agent — Cursor）

- 对照 `CLAUDE.md` / 公司 harness：边界、安全、i18n。
- Critical → 打回 Implementer；Medium → 记入 PR body。

### Hermes Worker（可选）

- 用途：GDPR/隐私字段评审、依赖漏洞解读、PR 安全评论。
- **禁止**：调度整条流水线；**关闭**无人值守下的技能自进化。

### OpenClaw Worker（可选）

- 用途：竞品页抓取、出海合规清单初筛、文档镜像。
- **禁止**：合并代码；顶层编排。

---

## 权限矩阵

| 能力 | CEO | Orchestrator | Cursor | Hermes | OpenClaw |
|------|:---:|:------------:|:------:|:------:|:--------:|
| 决定下一步流程 | ✓ | ✓ | ✗ | ✗ | ✗ |
| 写业务代码 | ✗* | ✗ | ✓ | ✗ | ✗ |
| 跑测试/lint | ✗ | ✓ | ✓ | ✗ | ✗ |
| merge 到 main | ✓** | 白名单 | ✗ | ✗ | ✗ |
| 读生产密钥 | ✓ | ✗ | ✗ | ✗ | ✗ |

\* emergency 除外  
\*\* 或 bot auto-merge 在白名单内

---

## 子 Agent 文件位置（公司标准）

复制到每个项目仓库：

```text
.cursor/agents/
  orchestrator.md
  planner.md
  implementer.md
  verifier.md
  reviewer.md
```

Multica 本仓实例：仓库根 `.cursor/agents/`（与 `.delivery/prompts/orchestrator-kickoff.md` 配套）。

---

## 相关文档

- [04-architecture.md](./04-architecture.md)  
- [05-stack-selection.md](./05-stack-selection.md)  
