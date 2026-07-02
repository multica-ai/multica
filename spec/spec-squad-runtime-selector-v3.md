# Spec v3：新建 Issue 时 Squad 分配路径的运行时选择器

## 修订记录

| 版本 | 日期 | 说明 |
| --- | --- | --- |
| v1 | 2026-06-24 | 首次方案设计 |
| v2 | 2026-06-25 | 基于实现现状修正，修复 `useMemo` deps bug，补全测试计划 |
| v3 | 2026-06-25 | 基于最终实现状态整理，作为单份权威参考文档 |

## 背景

参考 `spec.md`（解除智能体与本地运行时所有权绑定），该 spec 确立了核心原则：

- **智能体不绑定具体运行时**：创建智能体时只声明 `runtime_provider` / `runtime_profile_id` 能力要求。
- **本地执行跟随用户**：谁触发，就用谁自己的本地运行时执行。
- **运行时严格私有**：只有运行时 `owner_id` 本人可以看见、选择、调用；workspace owner/admin 也不例外。
- **没有兜底运行时**：找不到当前用户兼容运行时则入队失败。
- **运行时日志与运行时分离**：日志可按业务权限查看，但不等于运行时可见/调用权。

在此框架下，`spec.md` 已规定 issue 创建/分配时若 assignee 为 agent，前端应展示运行时选择器，让用户从自己拥有的兼容运行时中选一个。

**本 spec 补全 squad 分配路径**：当 assignee 为 squad（小队）时，后端将任务路由到 squad 的 leader agent。前端需要展示运行时选择器——让当前用户选择用哪个兼容运行时执行 leader agent 的任务。

## 问题

### 产品问题

创建 issue 时，若 assignee 选为 squad，前端不会展示运行时选择器。对比：若 assignee 直接选 agent，前端会展示运行时下拉框，让用户从自己拥有的、与该 agent 兼容的在线本地运行时中选一个——这与 `spec.md` 中"用户 B 触发分配时使用 B 的运行时"的设计一致。squad 路径缺失了这一节点。

### v2 已知 Bug（已修复）

| Bug | 描述 | 修复状态 |
| --- | --- | --- |
| `useMemo` deps 依赖 `selectedAgent` 而非 `selectedAgentForRuntime` | `compatibleRuntimes` 在 squad 路径下不会正确重新计算 | ✅ 已修复（依赖数组改为 `[currentUserId, runtimes, selectedAgentForRuntime]`） |
| 测试未覆盖 squad 路径 | 缺少 `squadListOptions` mock 和 squad 运行时选择器测试 | ✅ 已补全 |
| `api.ts` 注释未更新 | `CreateIssueRequest.runtime_id` 注释仍写 `agent assignees` | ✅ 已更新 |

## 目标行为

1. 用户在创建 issue 表单中选择一个小队作为 assignee。
2. 前端自动解析该小队的 leader agent（通过 `squad.leader_id`）。
3. 前端计算：当前用户拥有哪些**在线、且与 leader agent 的 `runtime_provider` / `runtime_profile_id` 兼容**的本地运行时。
4. 若有兼容运行时，展示运行时下拉选择器（与 agent assignee 路径视觉一致）——多个时默认选中排序后的第一个。
5. 若用户没有任何兼容运行时，提交按钮禁用并给出提示。
6. 提交时将选中的 `runtime_id` 写入 `CreateIssueRequest`。

**关键约束**：下拉框中列出的始终是**当前用户自己的**运行时，只是按 leader agent 的能力要求做了兼容性过滤。绝不会暴露 leader agent 本人或其他成员的运行时。

## 隐私与权限

本 spec 完全沿用 `spec.md` 既定的运行时隐私模型：

| 角色 | squad 路径能否看见运行时 | squad 路径能否选择/调用 |
| --- | --- | --- |
| 当前请求者（issue 创建者） | 仅能看见**自己**的兼容运行时 | 仅能选择**自己**的兼容运行时 |
| 小队队长（若非请求者） | 不可 | 不可 |
| workspace 其他成员 | 不可 | 不可 |
| workspace owner/admin（若非请求者） | 不可 | 不可 |

权限矩阵不因 squad 路径而放宽。运行时始终是 `owner_id` 的私有资源。

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
       ├── 有兼容运行时 → 渲染下拉框，默认选中第一个
       │                         │
       │                         ▼
       │              用户提交 → CreateIssueRequest { runtime_id: selected }
       │                         │
       │                         ▼
       │              后端: enqueueSquadLeaderTask(selectedRuntimeID)
       │                         │
       │                         ▼
       │              resolveRuntimeForTask: owner 校验 + 兼容性匹配
       │                         │
       │                         ▼
       │              写入 agent_task_queue.runtime_id
       │
       └── 无兼容运行时 → 禁用提交按钮，提示用户
```

## 实现方案

### 后端

**无需改动。** 现有链路已完整支持：

- `server/internal/service/issue.go`：`enqueueSquadLeaderTask` → `EnqueueTaskForSquadLeaderByRequesterWithRuntime` 已接受 `selectedRuntimeID`。
- `server/internal/service/task.go`：`resolveRuntimeForTask` 覆盖 owner 校验和兼容性匹配。

### 前端改动

#### `packages/views/modals/create-issue.tsx`

| # | 改动 | 行号 | 说明 |
| --- | --- | --- | --- |
| 1 | 引入 `squadListOptions` | 50 | 在 `ManualCreatePanel` 中查询 squad 列表 |
| 2 | 新增 `selectedAgentForRuntime` useMemo | 228-241 | agent 路径：按 `assigneeId` 查 agents；squad 路径：按 `leader_id` 查 leader agent |
| 3 | 修改 `compatibleRuntimes` deps | 252 | 依赖从 `selectedAgent` 改为 `selectedAgentForRuntime` |
| 4 | 修改 `selectedRuntimeId` 清空条件 | 255 | `assigneeType !== "agent" && assigneeType !== "squad"` 才清空 |
| 5 | 提交时 `runtime_id` 写入条件 | 309 | `assigneeType === "agent" || assigneeType === "squad"` |
| 6 | 运行时选择器渲染条件 | 625 | `assigneeType === "agent" || assigneeType === "squad"` |
| 7 | `manualCreateSubmitDisabled` | 87 | squad 路径下也要求 `selectedRuntimeId` 非空 |
| 8 | 提交 payload 中 squad 标识 | 484 | squad 路径下传 `squad_id` 而非 `agent_id` |

#### `packages/core/types/api.ts`

| # | 改动 | 行号 | 说明 |
| --- | --- | --- | --- |
| 1 | `CreateIssueRequest.runtime_id` 注释扩展 | 13 | "Per-run runtime selection for agent / squad assignees (squad path resolves via leader agent)" |
| 2 | `UpdateIssueRequest.runtime_id` 注释扩展 | 29 | 同上 |

#### `packages/views/modals/create-issue.test.tsx`

| # | 改动 | 行号 | 说明 |
| --- | --- | --- | --- |
| 1 | 新增 `mockSquadsData` hoisted 数据 | 68 | `squad-1` 包含 `leader_id: "agent-1"` |
| 2 | `vi.mock` 中注入 `squadListOptions` | — | mock 返回 `mockSquadsData.list` |
| 3 | "forwards the picked squad when switching to agent mode" 测试 | 670 | 验证 squad → agent 切换时保留 squad_id |
| 4 | `describe("squad runtime selector")` 测试块 | 912 | 4 个测试用例 |

#### 4 个 Squad 运行时选择器测试用例

| 测试 | 场景 | 验证点 |
| --- | --- | --- |
| 1 | squad + 有兼容运行时 | 下拉框出现，默认选中 "runtime-1" |
| 2 | squad + 无兼容运行时 | 下拉框隐藏，提交按钮禁用 |
| 3 | squad + 默认运行时提交 | `mockCreateIssue` 被调用，`runtime_id: "runtime-1"` |
| 4 | squad + 手动选择第二个运行时提交 | `mockCreateIssue` 被调用，`runtime_id: "runtime-2"` |

## 验收标准

1. 在 `create-issue.tsx` 中选中某 squad（其 leader agent 有 `runtime_provider` 能力要求）→ 出现运行时下拉框。
2. 下拉框仅列出当前用户拥有的、在线、兼容 leader agent 的本地运行时。
3. 默认选中第一个兼容运行时。
4. 手动选第二个运行时 → 提交 issue 后，后端 task 的 `runtime_id` 对应所选运行时。
5. 若用户没有兼容运行时，表单提交按钮禁用（与 agent 路径一致）。
6. `pnpm typecheck` 通过，无 TypeScript 错误。
7. `pnpm test` 通过，所有 squad 运行时选择器测试用例通过。

## 自测命令

```bash
pnpm typecheck
pnpm test
```

## 风险

- **TanStack Query 缓存**：`AssigneePicker` 打开时已 fetch squad 数据，`ManualCreatePanel` 新增 `useQuery(squadListOptions(...))` 复用缓存，无额外请求。
- **Leader agent 不可用**（已归档/被移除）：`agents.find` 返回 undefined，`selectedAgentForRuntime` 为 undefined → `compatibleRuntimes` 为空 → 下拉框不出现，行为与 agent 路径一致。
- **Leader agent 的 provider/profile 变更**：用户已选择 squad 后若 leader agent 配置变更，`compatibleRuntimes` 自动重新计算，下拉框选项随之更新。
- **React 18 严格模式**：`useMemo` 在严格模式下可能多次执行，但依赖数组修复后计算结果幂等，无副作用。

## 兼容性

- 不改变后端 API。
- 不改变 squad 数据模型。
- 不改变运行时隐私模型。
- 现有 agent assignee 路径的运行时选择器行为完全不受影响。
