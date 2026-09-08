# 04 — 技术架构

## 分层图（公司级）

```text
┌─────────────────────────────────────────────────────────────────────────┐
│ L0  CEO 层                                                                │
│  brief · 预算 · merge-policy · project-registry · BLOCKED 答复            │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ L1  任务公司层（Multica 或等价）                                           │
│  Workspace · Issue/Kanban · Task 生命周期 · Autopilot · 多 Runtime 并发    │
│  Webhook 入队 · 活动日志 · 人工指派                                       │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ L2  编排内核（硬 DAG — 必须代码驱动）                                      │
│  Phase 1: GitHub Actions + shell（队列 <10 ticket/天）                    │
│  Phase 2: LangGraph（细粒度分支、checkpoint、HITL）                        │
│  读 pytest/ruff/oasdiff 退出码 · 最大重试 · 状态持久化                     │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ L3  执行 Worker 池（只产出工件，无流程权）                                  │
│  cursor-agent（主力）· claude/codex（备选）· hermes（评审）· openclaw（情报）│
│  git worktree / Cloud VM 隔离 · 一 ticket 一 branch                     │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ L4  门禁与发布层（只信 exit code）                                         │
│  Verifier 子 Agent · make check / pnpm test · OpenAPI（vacuum/oasdiff）   │
│  Playwright E2E · GitHub required checks · merge-policy auto-merge      │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ L5  观测与告警                                                            │
│  Slack · Multica run 历史 · GHA artifacts · 成本仪表盘                     │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 单 ticket 数据流

```text
1. CEO 创建 Issue（agent-safe 模板）或 Autopilot 从 webhook 创建
2. 调度层选中 ticket → `dispatch-cursor-agent-cli.sh` 或 Multica assign
3. Cloud Agent / daemon 读：
      CLAUDE.md + .delivery/* + .ai-company/templates 约定 + Issue 正文
4. 子流水线：Planner → Implementer → Verifier → Reviewer
5. 开 PR（cursor/* 分支）
6. CI：frontend + backend + contract + e2e（按项目裁剪）
7. agent-delivery-gate：merge-policy.json → auto-merge 或等 CEO
8. 打 label agent-done；Multica issue → review/done
```

---

## 真相源（Source of Truth）

| 类型 | 文件 | 所有者 |
|------|------|--------|
| 做什么 | `brief.md` | CEO 起草，Planner 细化 |
| 怎么验收 | `accept_cases.md` | CEO 勾选项 |
| API 契约 | `api_spec.openapi.yaml` | 架构约束 |
| 怎么做 | `plan.md` | Planner |
| 能不能合并 | CI + `merge-policy.json` | 机器 |
| 公司宪法 | `.ai-company/docs/*` | CEO 修订 |

**聊天、Agent 记忆、Hermes 自进化技能 — 均不是真相源。**

---

## 多项目部署拓扑

```text
                    ┌──────────────────┐
                    │ Multica 自托管实例  │
                    │ (单 workspace 或多) │
                    └────────┬─────────┘
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
   repo: music-game    repo: landing-a     repo: saas-b
   .delivery/*         .delivery/*         .delivery/*
   共享 harness 模板    共享 harness 模板    共享 harness 模板
```

每个 repo：

- 复制 `.delivery/` harness（或 submodule `company-harness`）
- 登记到 `project-registry.yaml`
- 独立 `merge-policy.json`（可按风险调整 deny 列表）

---

## 反模式（架构级）

| 反模式 | 后果 |
|--------|------|
| LLM Orchestrator 决定是否跑测试 | 幻觉跳过门禁 |
| Multica 感知不到 lint 细粒度 | 整任务重试，浪费配额 |
| 每项目不同子 Agent 定义且不版本化 | 不可复制、不可审计 |
| Worker 直连 merge | 绕过 review |
| 无 worktree/分支隔离 | 多 Agent 互相踩文件 |

---

## 与 LangGraph 的接缝

LangGraph **不替代** Multica，而是：

- LangGraph 节点 `dispatch_to_multica(issue_id)` → webhook / CLI
- Multica 完成后 webhook 回调 LangGraph → 读 CI 状态 → 条件边

详见 [11-langgraph-when-and-how.md](./11-langgraph-when-and-how.md)。

---

## 相关文档

- [05-stack-selection.md](./05-stack-selection.md)  
- [07-quality-gates.md](./07-quality-gates.md)  
- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md)  
