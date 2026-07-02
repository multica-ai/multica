# Spec：新建 Issue 时小队队长的运行时选择

## 问题

当前 `create-issue.tsx` 和 `quick-create-issue.tsx` 中，当用户选择"小队"（squad）作为 assignee 时，运行时下拉框显示的**当前用户的本地运行时**，而不是**小队队长的本地运行时**。

这导致两个问题：
1. 用户看不到小队队长的可用运行时列表，无法为小队任务选择合适的运行时
2. 如果用户自己没有兼容的运行时，即使队长有在线运行时，也会报 "no compatible runtime" 错误

## 当前行为分析

### 后端（已正确实现 — 无需改动）
1. `GET /api/squads/:id/leader/compatible-runtimes` — 已存在，正确返回队长兼容的运行时列表
   - 查询条件：`workspace_id`、队长 `owner_id`、`runtime_mode='local'`、`status='online'`
   - 与队长 agent 的 `runtime_provider`/`runtime_profile_id` 能力匹配
2. `RuntimeResolver.Resolve()` 的 `AgentOwnerID` 字段 — 已存在，squad 场景使用队长 owner id 校验运行时归属
3. `preflightSquadLeaderRuntime()` — CreateIssue 流程中已正确调用
4. `EnqueueTaskForSquadLeaderByRequesterWithRuntime()` — 已支持显式传入 `selectedRuntimeID`

### 前端 create-issue.tsx（有 Bug）
```tsx
const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
// runtimeListOptions 返回的是当前用户的运行时（owner: "me"）

const compatibleRuntimes = useMemo(() => {
  if (!selectedAgentForRuntime) return [];
  return runtimes.filter(
    (runtime) =>
      firstCompatibleRuntimeForAgent(selectedAgentForRuntime, [runtime], {
        ownerId: currentUserId ?? null, // ← BUG: 这里用当前用户而不是队长
        onlineOnly: true,
      }) !== null,
  );
}, [currentUserId, runtimes, selectedAgentForRuntime]);
```

关键问题点：
- `runtimeListOptions(wsId)` 传递 `owner: "me"`，只返回当前用户的运行时
- `firstCompatibleRuntimeForAgent` 加上 `ownerId: currentUserId` 再次限制，始终显示当前用户的运行时
- 但小队任务实际会路由到队长 agent，应该显示**队长的**运行时
- `AssigneePicker` 调用时传了 `runtimeChoice={false}`，禁用了 picker 内建的小队运行时选择

### 前端 assignee-picker.tsx（已正确实现）
- 当 `runtimeChoice=true` 时，选择小队后会调用 `squadLeaderRuntimeOptions(wsId, squadId)` 查询队长的运行时
- 显示队长的运行时让用户选择
- 队长有且只有一个运行时自动选中

### 前端 quick-create-issue.tsx（有同样 Bug）
- 使用 `runtimeListOptions(wsId)` 筛选运行时
- 当选择小队时，同样错误地显示当前用户的运行时

## 解决方案

### 方案说明

**create-issue.tsx 采用方案 A**：保留现有的运行时 `<select>` 控件，但在 assigneeType 为 "squad" 时改用 `squadLeaderRuntimeOptions` 查询队长的运行时。

**quick-create-issue.tsx 采用方案 B**：利用已有的 `assignee-picker.tsx` 处理小队运行时选择（传 `runtimeChoice=true`）——因为 quick-create 的运行时选择逻辑完全内嵌在 assignee picker 中。

### 后端改动

无需改动。现有 `ListSquadLeaderCompatibleRuntimes` 和 `RuntimeResolver.AgentOwnerID` 已正确实现。

### 前端改动

#### 1. packages/views/modals/create-issue.tsx

**改动点：**
- 新增 `squadLeaderRuntimeOptions` 查询，在 assigneeType 为 "squad" 且 assigneeId 有效时启用
- 修改 `compatibleRuntimes` 的计算逻辑，区分 agent 和 squad 场景
  - agent 场景：保持原有逻辑（`runtimeListOptions + firstCompatibleRuntimeForAgent`）
  - squad 场景：使用 `squadLeaderRuntimeOptions` 返回的结果
- `selectedRuntimeId` 状态管理和 `<select>` 渲染逻辑保持不变（仅数据源改变）

```tsx
// 新增导入
import { squadLeaderRuntimeOptions } from "@multica/core/squads";

// 新增 squad 运行时查询
const squadRuntimesQuery = useQuery({
  ...squadLeaderRuntimeOptions(wsId, assigneeId ?? ""),
  enabled: assigneeType === "squad" && !!assigneeId,
});
const squadRuntimes = squadRuntimesQuery.data ?? [];

// 修改 compatibleRuntimes
const compatibleRuntimes = useMemo(() => {
  if (assigneeType === "squad") {
    return squadRuntimes;
  }
  if (!selectedAgentForRuntime) return [];
  return runtimes.filter(
    (runtime) => firstCompatibleRuntimeForAgent(
      selectedAgentForRuntime, [runtime], {
        ownerId: currentUserId ?? null,
        onlineOnly: true,
      },
    ) !== null,
  );
}, [assigneeType, squadRuntimes, currentUserId, runtimes, selectedAgentForRuntime]);
```

#### 2. packages/views/modals/quick-create-issue.tsx

**改动点：**
- 在 assignee picker 中确保 `runtimeChoice` 默认开启（让 picker 内建的小队运行时选择逻辑生效）
- 确认 quick-create 提交时 `runtime_id` 被正确从 actor/assignee pick 中携带

#### 3. packages/views/issues/components/pickers/assignee-picker.tsx

**改动点：**
- 无需改动，已有正确的 squad 运行时选择逻辑

### 测试验证

#### 单元测试
- `create-issue.test.tsx`：测试 squad 选择后运行时下拉显示队长的运行时
- `quick-create-issue.test.tsx`：测试 squad 选择后运行时选择正确

#### 手动测试场景
1. 创建小队 A，队长 C 有一个本地在线运行时
2. 队员 B（没有运行时）创建 issue，选择 assignee = 小队 A
3. 验证：运行时下拉框显示的是队长 C 的运行时列表（而不是空的或队员 B 的）
4. 队员 B 选择一个运行时，提交 issue
5. 验证：issue 创建成功，task 入队到队长 C 选定的运行时

### 注意事项

- **与 spec.md 的一致性**：本次改动符合 spec.md 中"issue 创建/分配时允许用户选择自己的本地运行时"的原则，但 squad 场景特殊——实际执行者是队长，所以选择的是**队长的本地运行时**。这已经在后端通过 `AgentOwnerID` 字段处理。
- **运行时私有性**：`squadLeaderRuntimeOptions` 返回的是队长自己的运行时，队长之外的其他人不可见。但队长可以查看并选择。队员在选择小队时只能看到队长的运行时名称列表，看不到运行时的详细信息（如配置、连接信息等）。
- **无运行时的行为**：如果队长没有兼容的在线运行时，下拉框不显示，提示"小队队长没有兼容的在线运行时，请队长先启动运行时"。
- **backend 已有的正确性**：`preflightSquadLeaderRuntime` 使用 `ResolveRuntimeForAgentByRequester`，其内部 `RuntimeResolver` 的 `AgentOwnerID` 会正确使用队长的 OwnerID 进行运行时归属校验。
