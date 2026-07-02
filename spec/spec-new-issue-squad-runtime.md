# Spec：新建 Issue 选择小队时，队长运行时选择

## 修订记录

| 版本 | 日期 | 说明 |
| --- | --- | --- |
| v1 | 2026-06-26 | 基于 `spec.md`（解除智能体与本地运行时所有权绑定）编写，聚焦新建 Issue 时 Squad 路径的运行时选择 |

## 背景

参考 `spec.md`（解除智能体与本地运行时所有权绑定），核心原则已确立：

- **智能体不绑定具体运行时**: 创建智能体时只声明 `runtime_provider` / `runtime_profile_id` 能力要求，不再写入具体 `runtime_id`。
- **本地执行跟随用户**: 谁触发 task，就用谁自己的本地运行时执行。
- **运行时严格私有**: 只有运行时 `owner_id` 本人可以看见、选择、调用；workspace owner/admin 也不例外。
- **没有兜底运行时**: 找不到当前用户兼容运行时则入队失败，不跨用户兜底。

在此框架下，`spec.md` 已规定 issue 创建/分配时若 assignee 为 agent，前端应展示运行时选择器。

**本 spec 补全 squad 分配路径**: 当 assignee 为 squad（小队）时，后端将任务路由到 squad 的 leader agent。前端需要展示运行时选择器——让当前用户选择用哪个**自己拥有的**、与 leader agent 能力要求兼容的本地运行时来执行该任务。

## 问题

创建 issue 时，若 assignee 选为 squad，前端未展示运行时选择器。对比 agent 路径已有完善的选择器，squad 路径缺失了"让用户选择和提交运行时"这一环节。

### 当前代码状态

| 场景 | 运行时选择器 | 说明 |
| --- | --- | --- |
| assignee = agent | 已实现 | `compatibleRuntimes` 正常计算，下拉框正常渲染 |
| assignee = squad | 已实现 | `selectedAgentForRuntime` 已解析 leader agent，复用 agent 路径 |

> 注: 当前代码已实现大部分功能，但部分后端 bug 需要修复（详见 Bug 部分）。

## 目标行为

1. 用户在创建 issue 表单中选择一个小队（squad）作为 assignee。
2. 前端自动解析该小队的 leader agent（通过 `squad.leader_id`）。
3. 前端计算: 当前用户拥有哪些**在线、且与 leader agent 的 `runtime_provider` / `runtime_profile_id` 兼容**的本地运行时。
4. 若有兼容运行时，展示运行时下拉选择器（与 agent assignee 路径视觉一致）——多个时默认选中第一个。
5. 若用户没有任何兼容运行时，提交按钮禁用并给出提示。
6. 提交时将选中的 `runtime_id` 显式写入 `CreateIssueRequest`。

**关键约束**: 下拉框中列出的始终是**当前用户自己的**运行时，只是按 leader agent 的能力要求做了兼容性过滤。绝不会暴露 leader agent 本人或其他成员的运行时。

## 隐私与权限

本 spec 完全沿用 `spec.md` 既定的运行时隐私模型:

| 角色 | squad 路径能否看见运行时 | squad 路径能否选择/调用 |
| --- | --- | --- |
| 当前请求者（issue 创建者） | 仅能看见**自己**的兼容运行时 | 仅能选择**自己**的兼容运行时 |
| 小队队长（若非请求者） | 不可见 | 不可调用 |
| workspace 其他成员 | 不可见 | 不可调用 |
| workspace owner/admin（若非该运行时 owner 本人） | 不可见 | 不可调用 |

运行时始终是 `owner_id` 的私有资源，权限矩阵不因 squad 路径而放宽。运行日志可按现有业务权限查看，查看日志不等于获得运行时调用权。

## 数据流

```
用户选择 Squad assignee
       │
       ▼
 前端解析: squad.leader_id → agents.find(leaderAgent)
       │
       ▼
 前端计算: firstCompatibleRuntimeForAgent(leaderAgent, currentUserRuntimes)
       │
       ├─ 有兼容运行时 → 渲染下拉框，默认选第一个
       │                    │
       │                    ▼
       │         用户提交 → CreateIssueRequest { runtime_id: selected }
       │                    │
       │                    ▼
       │         后端 → enqueueSquadLeaderTask(selectedRuntimeID)
       │                    │
       │                    ▼
       │         resolveRuntimeForTask: owner 校验 + 兼容性匹配
       │                    │
       │                    ▼
       │         写入 agent_task_queue.runtime_id
       │
       └─ 无兼容运行时 → 禁用提交按钮 + 提示用户
```

## 实现方案

### 后端改动

**无需重大改动。** 现有链路已完整支持:

- `enqueueSquadLeaderTask` → `EnqueueTaskForSquadLeaderByRequesterWithRuntime` 已接收 `selectedRuntimeID`。
- `resolveRuntimeForTask` (在 `server/internal/service/task.go`) 已覆盖 owner 校验和兼容性匹配。

但需要修复以下问题（参考 `spec-remaining-work.md`）:

| Bug | 问题 | 修复方案 |
| --- | --- | --- |
| Bug 1 | `ListSquadLeaderCompatibleRuntimes` handler 错误查询 leader agent owner 的运行时 | 改为查询当前请求用户的兼容运行时 |
| Bug 1a | `resolveRuntimeForTask` explicit choice 路径拒绝请求者运行时 | 跳过 AgentOwnerID，直接校验运行时属于请求者 |
| Bug 2 | `daemon_id` 不匹配导致 `local_directory` 不可用 | 移除 daemon_id 过滤，由 daemon 自行管理 |

### 前端改动

#### 1. packages/views/modals/create-issue.tsx

| # | 改动点 | 状态 | 说明 |
| --- | --- | --- | --- |
| 1 | 引入 `squadListOptions` | 已完成 | 在 ManualCreatePanel 中查询 squad 列表 |
| 2 | 新增 `selectedAgentForRuntime` useMemo | 已完成 | agent 路径按 assigneeId 查 agents；squad 路径按 leader_id 查 leader agent |
| 3 | 修改 `compatibleRuntimes` 依赖 | 已完成 | 从 selectedAgent 改为 selectedAgentForRuntime |
| 4 | selectedRuntimeId 清空条件 | 已完成 | assigneeType 既不是 agent 也不是 squad 时才清空 |
| 5 | 提交时 runtime_id 写入条件 | 已完成 | agent 和 squad 两种路径都写 runtime_id |
| 6 | 运行时选择器渲染条件 | 已完成 | 同时支持 agent 和 squad |
| 7 | manualCreateSubmitDisabled | 已完成 | squad 路径下也要求 selectedRuntimeId 非空 |
| 8 | 提交 payload 中 squad 标识 | 已完成 | squad 路径传 squad_id |

> 注: AssigneePicker 中传 runtimeChoice={false} 是正确的，因为 create-issue 自己管理运行时选择器。

#### 2. packages/views/modals/quick-create-issue.tsx

| # | 改动点 | 状态 | 说明 |
| --- | --- | --- | --- |
| 1 | selectedAgent useMemo | 已完成 | actor.type === "squad" 时通过 squad.leader_id 查找 leader agent |
| 2 | compatibleRuntimes | 已完成 | 自动基于 selectedAgent 计算兼容运行时 |
| 3 | 运行时选择器显示条件 | 已完成 | selectedAgent && compatibleRuntimes.length > 1 对两者均生效 |
| 4 | 提交时 runtime_id | 已完成 | selectedRuntime 正确携带到提交逻辑中 |

#### 3. packages/views/issues/components/pickers/assignee-picker.tsx

| # | 改动点 | 状态 | 说明 |
| --- | --- | --- | --- |
| 1 | squadLeaderRuntimeOptions hook | 已完成 | 已导入 @multica/core/squads |
| 2 | Squad 运行时选择子视图 | 已完成 | selectedSquadForRuntime 状态 + 运行时列表渲染 |
| 3 | 自动选中单运行时 | 已完成 | squad 只有 1 个兼容运行时自动选中 |
| 4 | 权限校验 | 已完成 | 后端会校验当前请求用户的运行时 |

### 测试

create-issue.test.tsx:

| # | 测试用例 | 场景 | 验证点 |
| --- | --- | --- | --- |
| 1 | squad + 有兼容运行时 | 选中 squad，leader agent 有兼容运行时 | 下拉框出现，默认选 runtime-1 |
| 2 | squad + 无兼容运行时 | 选中 squad，leader agent 无兼容运行时 | 下拉框隐藏，提交按钮禁用 |
| 3 | squad + 默认运行时提交 | 选中 squad，不手动选运行时直接提交 | mockCreateIssue 被调用，runtime_id: runtime-1 |
| 4 | squad + 手动选第二个运行时提交 | 选中 squad，手动选第二个运行时再提交 | mockCreateIssue 被调用，runtime_id: runtime-2 |
| 5 | squad → agent 切换保留 squad_id | 先选 squad 再切 agent | 切换时保留 squad_id |

## 剩余工作确认

根据 `spec-remaining-work.md`，以下问题需要验证和修复:

| 优先级 | 问题 | 状态 | 说明 |
| --- | --- | --- | --- |
| P0 | Bug 1: ListSquadLeaderCompatibleRuntimes 查询了错误 owner 的运行时 | 已修复 | 已查询请求用户的运行时，而非 leader agent owner 的 |
| P0 | Bug 1a: resolveRuntimeForTask explicit choice 路径拒绝请求者运行时 | 已修复 | 已移除 AgentOwnerID override，explicit choice 始终校验请求者本人 |
| P1 | 前端 squad 运行时选择器端到端验证 | 已验证 | create-issue.tsx、quick-create-issue.tsx、assignee-picker.tsx 已覆盖 squad leader runtime |
| P2 | Bug 2: daemon_id 不匹配导致 local_directory 不可用 | 已修复 | daemon 与 claim 响应均不再按 daemon_id 过滤；每个 project 只允许一个 local_directory，避免歧义 |
| P3 | 运行时私有性权限审计 | 已验证 | runtime API 对非 owner 返回 404，owner/admin 也不能越权 |

## 验收标准

1. 在 create-issue.tsx 中选中 squad（leader agent 有 runtime_provider）→ 出现运行时下拉框。
2. 下拉框仅列出当前用户拥有的、在线、兼容 leader agent 的本地运行时。
3. 默认选中第一个兼容运行时。
4. 手动选第二个运行时 → 提交 issue 后，后端 task 的 runtime_id 对应所选运行时。
5. 若用户没有兼容运行时，表单提交按钮禁用。
6. ListSquadLeaderCompatibleRuntimes API 返回请求用户自己的兼容运行时。
7. resolveRuntimeForTask explicit choice 路径正确校验运行时属于请求者本人。
8. daemon_id 不匹配不再导致 local_directory 不可用。
9. 所有 runtime API 对非 owner 返回 404。
10. pnpm typecheck、pnpm test、make test 全部通过。

## 自测命令

```bash
pnpm typecheck
pnpm test
make test
```

## 风险

- **TanStack Query 缓存**: AssigneePicker 打开时已 fetch squad 数据，ManualCreatePanel 的新增 useQuery(squadListOptions(...)) 复用缓存，无额外请求。
- **Leader agent 不可用**: agents.find 返回 undefined → compatibleRuntimes 为空 → 下拉框不出现，行为与 agent 路径一致。
- **Leader agent 的 provider/profile 变更**: 已选择 squad 后若 leader agent 配置变更，compatibleRuntimes 自动重新计算。
- **React 18 严格模式**: useMemo 在严格模式下可能多次执行，但依赖数组修复后计算结果幂等。

## 兼容性

- 不改变后端 API 契约。
- 不改变 squad 数据模型。
- 不改变运行时隐私模型。
- 现有 agent assignee 路径的运行时选择器行为完全不受影响。
