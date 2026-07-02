 # Spec v2：新建 Issue 时 Squad 分配路径的运行时选择器
 
 ## 修订记录
 
 | 版本 | 日期 | 说明 |
 | --- | --- | --- |
 | v1 | — | 首次方案设计（合并到 `spec-squad-runtime-selector.md`） |
 | v2 | 2026-06-25 | 基于实现现状的修正版 spec，修正已知 bug 并补全测试计划 |
 
 ## 背景
 
 参考 `spec.md`（解除智能体与本地运行时所有权绑定），该 spec 确立了以下核心原则：
 
 - **智能体不绑定具体运行时**：创建智能体时只声明 `runtime_provider` / `runtime_profile_id` 能力要求。
 - **本地执行跟随用户**：谁触发，就用谁自己的本地运行时执行。
 - **运行时严格私有**：只有运行时 `owner_id` 本人可以看见、选择、调用；workspace owner/admin 也不例外。
 - **没有兜底运行时**：找不到当前用户兼容运行时则入队失败，不使用他人运行时、workspace public 运行时或 cloud 运行时。
 - **运行时日志与运行时权限分离**：日志可按业务权限查看，但不等于运行时可见/调用权。
 
 在此框架下，`spec.md` 已规定 issue 创建/分配时若 assignee 为 agent，前端应展示运行时选择器，让用户从自己拥有的兼容运行时中选一个。
 
 **本 spec 补全 squad 分配路径**：当 assignee 为 squad（小队）时，后端会将任务实际路由到 squad 的 leader agent。此时前端同样需要展示运行时选择器——让当前用户选择"用我的哪个兼容运行时来执行该 leader agent 的任务"。
 
 ## 问题
 
 ### 产品问题
 
 当前创建 issue 时，若 assignee 选为 squad，前端不会展示运行时选择器。对比：若 assignee 直接选 agent，前端表单会展示一个运行时下拉框，让用户从自己拥有的、与该 agent 兼容的在线本地运行时中选一个——这与 `spec.md` 中"用户 B 触发分配时使用 B 的运行时"的设计一致。squad 路径缺失了这一节点。
 
 ### 实现现状（v2 发现的问题）
 
 `packages/views/modals/create-issue.tsx` 中，squad 运行时选择器的基础逻辑已由 v1 spec 实现，但存在**一处已知 bug**：
 
 #### Bug 1：`compatibleRuntimes` 的 useMemo deps 仍引用 `selectedAgent`
 
 **位置**：第 252 行
 
 **代码**：
 ```typescript
 const compatibleRuntimes = useMemo(() => {
   if (!selectedAgentForRuntime) return [];
   return runtimes.filter(
     (runtime) =>
       firstCompatibleRuntimeForAgent(selectedAgentForRuntime, [runtime], {
         ownerId: currentUserId,
         onlineOnly: true,
       }) !== null,
   );
 }, [currentUserId, runtimes, selectedAgent]);  // ← 应为 selectedAgentForRuntime
 ```
 
 **影响**：`compatibleRuntimes` 的 `useMemo` 依赖数组包含 `selectedAgent` 而非 `selectedAgentForRuntime`。`selectedAgent` 已在重构中更名为 `selectedAgentForRuntime`，但依赖数组未同步更新。
 
 - 当 `assigneeType === "agent"` 时：`selectedAgent` 和 `selectedAgentForRuntime` 值相同，行为正确（但不必要地重新计算）。
 - 当 `assigneeType === "squad"` 时：`selectedAgent` 是旧的 `useMemo` 计算值（仅在 agent 路径时有值），`selectedAgentForRuntime` 是新计算值。依赖数组中 `selectedAgent` 可能为 undefined 或旧值，导致 `compatibleRuntimes` 不会在 squad 路径下正确重新计算。
 - 实际上由于 `useMemo` 在 React 18 中默认浅比较依赖，如果 `selectedAgent`（旧变量）恰好是 undefined，而 `selectedAgentForRuntime` 有值，组件可能不会按预期重新计算兼容运行时列表。
 - **修复方法**：将依赖数组从 `[currentUserId, runtimes, selectedAgent]` 改为 `[currentUserId, runtimes, selectedAgentForRuntime]`。
 
 #### Bug 2（临界）：测试文件未覆盖 squad 路径
 
 `packages/views/modals/create-issue.test.tsx` 没有 `squadListOptions` mock，也没有 squad 相关的运行时选择器测试用例。
 
 #### 文档缺失：`api.ts` 注释未更新
 
 `packages/core/types/api.ts` 中 `CreateIssueRequest.runtime_id` 的注释仍写 `agent assignees`，未扩展为 `agent / squad assignees`。
 
 ## 目标行为
 
 1. 用户在创建 issue 表单中选择一个小队作为 assignee。
 2. 前端自动解析该小队的 leader agent（通过 `squad.leader_id`）。
 3. 前端计算：当前用户拥有哪些**在线、且与 leader agent 的 `runtime_provider` / `runtime_profile_id` 兼容**的本地运行时。
 4. 若有兼容运行时，展示运行时下拉选择器（与 agent assignee 路径视觉一致）：
    - 多个时默认选中排序后的第一个（在线优先 → 创建时间 → 名称）。
 5. 若用户没有任何兼容运行时，提交按钮禁用，并给出提示。
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
 
 ## 后端现状
 
 `server/internal/service/issue.go` 中的 `enqueueSquadLeaderTask` → `EnqueueTaskForSquadLeaderByRequesterWithRuntime` → `enqueueMentionTask` → `resolveRuntimeForTask` 链路已完整支持 `selectedRuntimeID` 的传入、owner 校验和兼容性匹配。
 
 **后端不需要改动。**
 
 ## 前端改动清单
 
 ### 【必须修复】`packages/views/modals/create-issue.tsx` — Bug 修复
 
 修正 `compatibleRuntimes` 的 `useMemo` 依赖数组，将 `selectedAgent` 改为 `selectedAgentForRuntime`。
 
 ```diff
   const compatibleRuntimes = useMemo(() => {
     if (!selectedAgentForRuntime) return [];
     return runtimes.filter(
       (runtime) =>
         firstCompatibleRuntimeForAgent(selectedAgentForRuntime, [runtime], {
           ownerId: currentUserId,
           onlineOnly: true,
         }) !== null,
     );
 -  }, [currentUserId, runtimes, selectedAgent]);
 +  }, [currentUserId, runtimes, selectedAgentForRuntime]);
 ```
 
 ### 【文档修复】`packages/core/types/api.ts`
 
 将 `CreateIssueRequest.runtime_id` 字段注释从"Per-run runtime selection for agent assignees"扩展为"Per-run runtime selection for agent / squad assignees（squad 路径下按 leader agent 解析）"。
 
 ```diff
 -  /** Per-run runtime selection for agent assignees. Must be owned by requester. */
 +  /** Per-run runtime selection for agent / squad assignees. Must be owned by requester. Squad path resolves via leader agent. */
   runtime_id?: string;
 ```
 
 ### 【必须修复】`packages/views/modals/create-issue.test.tsx` — 补全测试
 
 #### 新增 mock 数据
 
 在文件顶部 hoisted 区域新增：
 
 ```typescript
 const mockSquadsData = vi.hoisted(() => ({
   list: [{
     id: "squad-1",
     name: "Squad Alpha",
     leader_id: "agent-1",
   }] as Array<{
     id: string;
     name: string;
     leader_id: string;
   }>,
 }));
 ```
 
 #### 扩展 queries mock
 
 在 `vi.mock("@multica/core/workspace/queries")` 中新增 `squadListOptions`：
 
 ```diff
   vi.mock("@multica/core/workspace/queries", () => ({
     agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => Promise.resolve(mockAgentsData.list) }),
 +    squadListOptions: () => ({ queryKey: ["squads"], queryFn: () => Promise.resolve(mockSquadsData.list) }),
   }));
 ```
 
 #### 新增测试用例
 
 1. **基本 squad + 运行时选择**：用户选择 squad，当前用户有兼容 leader agent 的在线运行时 → 运行时下拉框出现，默认选中第一个。
 2. **squad 无兼容运行时**：用户选择 squad，当前用户无兼容运行时（例如 provider 不匹配或全部离线）→ 下拉框不出现，提交按钮禁用。
 3. **squad 手动选择第二个运行时**：用户选择 squad，下拉框出现后手动选第二个兼容运行时 → 提交请求中 `runtime_id` 为所选值。
 4. **squad → agent 切换**：用户从 squad 切换为 agent assignee → 运行时选择器正常切换（agent 的兼容运行时列表），不残留 squad 路径下的选中值。
 5. **squad → member 切换**：用户从 squad 切换为 member assignee → 运行时选择器消失，`runtime_id` 清空。
 6. **manualCreateSubmitDisabled squad 路径**：squad 路径下无 `selectedRuntimeId` 时返回 true；有值时返回 false。
 
 ## 自测验证命令
 
 ```bash
 pnpm typecheck           # TypeScript 编译检查，可发现 deps 中未定义的变量引用
 pnpm test                # 运行 Vitest 测试套件，验证 squad 运行时选择器行为
 ```
 
 ## 兼容性
 
 - 不改变后端 API。
 - 不改变 squad 数据模型。
 - 不改变运行时隐私模型。
 - 现有 agent assignee 路径的运行时选择器行为完全不受影响。修复 deps bug 后，agent 路径的计算行为也不应有变化（React 会在正确依赖下重新计算，结果不变）。
 
 ## 验收标准
 
 1. 在 `create-issue.tsx` 中选中某个 squad（其 leader agent 有 `runtime_provider` 能力要求）→ 出现运行时下拉框。
 2. 下拉框仅列出当前用户拥有的、在线、兼容 leader agent 的本地运行时。
 3. 默认选中第一个兼容运行时。
 4. 手动选第二个运行时 → 提交 issue 后，后端 task 的 `runtime_id` 对应所选运行时（通过 `EnqueueTaskForSquadLeaderByRequesterWithRuntime` 链路）。
 5. 若当前用户没有兼容运行时，表单提交按钮禁用（与 agent 路径一致）。
 6. `pnpm typecheck` 通过，无 TypeScript 错误。
 7. `pnpm test` 通过，所有 squad 运行时选择器测试用例通过。
 
 ## 风险
 
 - **TanStack Query 缓存**：`AssigneePicker` 打开时已 fetch squad 数据，TanStack Query 复用缓存，`ManualCreatePanel` 新增 squad 查询不会产生额外网络请求。
 - **Leader agent 不可用**（已归档/被移除）：`agents.find` 返回 undefined，`selectedAgentForRuntime` 为 undefined → `compatibleRuntimes` 为空 → 下拉框不出现，与 agent 路径中 agent 不可用的行为一致。
 - **Leader agent 的 provider/profile 变更**：用户已选择 squad 后若 leader agent 配置变更，`compatibleRuntimes` 会自动重新计算，下拉框选项随之更新。
 - **React 18 严格模式**：`useMemo` 在严格模式下可能多次执行，但依赖数组修复后计算结果应幂等，无副作用。
