 # Spec：新建 Issue 选择小队时，队长运行时选择

 ## 问题

 当前新建/编辑 Issue 时，`assignee_picker` 组件支持对智能体（agent）选择运行时：如果当前用户有多个兼容运行时，会展开运行时选择子视图让用户选择具体运行哪台机器。但选择小队（squad）作为 assignee 时，直接提交 `{ assignee_type: "squad", assignee_id: s.id }`，不携带 `runtime_id`。

 小队 assignee 的语义是：issue 实际路由到队长（leader agent）执行。队长智能体有自己的 `runtime_provider` / `runtime_profile_id` 能力要求。当前 select squad 后走的是后端 squad task 入队逻辑（`enqueueSquadLeaderTask`），该路径通过 task resolver 动态解析运行时，但前端没有给用户提前选择的机会。

 目标行为：

 - 新建/编辑 Issue 时，如果 assignee 选择了小队，且当前登录用户有多个兼容该小队队长智能体的本地运行时，则弹出一个运行时选择子视图让用户选择具体用哪个运行时来执行。
 - 如果只有 1 个兼容运行时，自动选中并随 `assignee_type` / `assignee_id` 一起提交 `runtime_id`。
 - 如果当前用户没有兼容运行时，则阻止提交 assignee，并显示清晰原因（例如"你没有兼容小队队长的本地运行时"）。
 - 与智能体运行时选择行为保持一致：选择逻辑、UI 交互、提交字段都对称。
 - 创建智能体本身不绑定具体运行时（按已有 spec 逻辑，智能体只保存 `runtime_provider` / `runtime_profile_id` 能力要求）。

 ## 相关现有能力（不需要重复造轮子）

 以下能力已存在，不会在本次 spec 中重新设计：

 1. **`runtime_id` API 字段已存在。**
    - `CreateIssueRequest` / `UpdateIssueRequest` 已有 `runtime_id?: string`，注释为"Per-run runtime selection for agent / squad assignees (squad path resolves via leader agent). Must be owned by requester."
    - 前端提交 `runtime_id` 后，后端会通过 resolver 校验该运行时属于请求者本人、在线、同工作区、满足 agent provider/profile 能力要求。

 2. **Assignee picker 已支持 agent 运行时选择子视图。**
    - 当前逻辑：点击 agent → `compatibleRuntimesForAgent(a)` 查出当前用户的兼容运行时 → 多个则显示子视图让用户选，1 个则自动带 `runtime_id`。

 3. **`ListSquadLeaderCompatibleRuntimes` API 已存在。**
    - 已有 `GET /api/squads/:id/leader/compatible-runtimes`，返回 `RuntimeBriefResponse[]`（仅 id + name）。
    - 已有对应的 React Query hook：`squadLeaderRuntimeOptions(wsId, squadId)`。

 4. **后端 squad task 入队时已有运行时解析逻辑。**
    - `enqueueSquadLeaderTask` 通过 task resolver 动态解析运行时，但当前没有利用前端传入的 `runtime_id`。

 ## 需要改动的地方

 ### 1. 后端：修改 `ListSquadLeaderCompatibleRuntimes` 返回请求者自己的兼容运行时

 **当前行为：**

 `ListSquadLeaderCompatibleRuntimes` 返回的是**队长智能体 owner**的兼容运行时，使用 `ListUserCompatibleRuntimes` 时传入的 `OwnerID` 是队长 agent 的 `owner_id`，而非当前请求用户。

 **需要改成：**

 返回**当前请求用户**的兼容运行时，同时满足：
 - `runtime.owner_id = 当前请求用户`
 - `runtime.status = 'online'`
 - 兼容队长智能体的 `runtime_provider` / `runtime_profile_id`
 - 在工作区范围内

 即把 handler 中的 `agentOwnerID` 替换为从 JWT 或请求上下文中取出的当前用户 ID。

 **为什么：**

 根据已有 spec 的运行时私有性设计，运行时属于个人，成员只能看见和调用自己的运行时。选择小队队长时，用户也需要用**自己的**本地运行时来执行队长任务，而不是用队长 owner 的运行时。这保持了"谁的 issue 谁用自己的运行时"的一致性。

 **改动文件：**

 - `server/internal/handler/squad.go`：`ListSquadLeaderCompatibleRuntimes` 函数

 ### 2. 后端：squad task 入队时优先使用前端传入的 `runtime_id`

 **当前行为：**

 `enqueueSquadLeaderTask` 没有读取前端可能传入的 `runtime_id`，直接走 task resolver 自动选择运行时。

 **需要改成：**

 如果 issue 创建/更新请求中包含了 `runtime_id`，则：
 - 校验该 `runtime_id` 属于请求者本人（`owner_id == 请求用户`）
 - 校验该运行时在线、同工作区、兼容队长 agent 的 provider/profile
 - 校验通过后直接使用该 `runtime_id` 创建 task
 - 校验失败则拒绝入队并返回错误原因

 如果请求中没有 `runtime_id`，则走现有的 task resolver 自动选择逻辑（向后兼容）。

 **改动文件：**

 - `server/internal/handler/squad.go`：`enqueueSquadLeaderTask` 或相关入队函数

 ### 3. 前端：选择小队时触发运行时选择

 **当前行为：**

 在 `assignee_picker.tsx` 中，选择 squad 时直接执行 `onUpdate({ assignee_type: "squad", assignee_id: s.id })`，没有运行时选择。

 **目标行为拆解：**

 点击小队后：

 **a) 获取小队队长智能体的兼容运行时列表**
    使用 `squadLeaderRuntimeOptions(wsId, s.id)` 查询 `GET /api/squads/:id/leader/compatible-runtimes`。
    - 如果列表长度 > 1：展开运行时选择子视图（与 agent 运行时选择的 UI 一致），让用户选择一个具体的运行时。
    - 如果列表长度 === 1：自动选中该运行时。
    - 如果列表长度为 0：显示错误提示"你没有兼容小队队长智能体的本地运行时，请先安装并启动兼容的本地运行时"，阻止提交 assignee。

 **b) 提交时携带 `runtime_id`**
    选择的运行时 ID 设置到 `runtime_id` 字段中：
    ```
    onUpdate({
      assignee_type: "squad",
      assignee_id: s.id,
      runtime_id: selectedRuntimeId,
    });
    ```

 **c) 子视图的后退导航**
    进入运行时选择子视图后，应在顶部显示返回箭头和该小队名称，让用户可以退回查看 squad 列表。

 **d) 兼容运行时的 UI 渲染**
    运行时条目用 `Cpu` 图标，显示运行时名称，与 agent 运行时选择的渲染一致。

 **交互流程（以 3 个兼容运行时为例）：**

 ```
 ┌─────────────────────────┐
 │ 🔍 搜索成员/智能体/小队   │
 ├─────────────────────────┤
 │ 未分配 (清除)            │
 ├─── 成员 ────             │
 │ ○ 张三                   │
 │ ○ 李四                   │
 ├─── 智能体 ───             │
 │ ○ Codex Agent            │
 │ ○ Helper Agent           │
 ├─── 小队 ───              │
 │ ▶ 前端团队               │  ← 用户点击
 └─────────────────────────┘

         ↓ 点击后（3 个兼容运行时）

 ┌─────────────────────────┐
 │ ← 前端团队               │  ← 返回按钮 + 小队名
 ├─── 运行时 ───            │
 │ 💻 Laptop               │  ← 用户当前自己的兼容运行时
 │ 💻 Desktop              │
 │ 💻 Workstation          │
 └─────────────────────────┘

          ↓ 用户选择了 Laptop

 提交: { assignee_type: "squad", assignee_id: "前端团队id", runtime_id: "Laptop-id" }
 ```

 **改动文件：**

 - `packages/views/issues/components/pickers/assignee-picker.tsx`

 ### 4. 前端：issue 详情页编辑 assignee 时同样支持

 **当前行为：**

 Issue 详情页的 assignee 编辑也使用同一个 `AssigneePicker` 组件（通过 `useUpdateIssue` mutation 提交 `UpdateIssueRequest`）。该组件已经支持 `runtimeChoice` prop，且 `UpdateIssueRequest` 已有 `runtime_id` 字段。

 **需要确认：**

 - AssigneePicker 在 issue 详情页编辑时，`runtimeChoice` 默认是 `true`，所以 squad 运行时选择在编辑场景也会自动生效。
 - 无需额外改动，只要 `assignee-picker.tsx` 实现 squad 运行时选择，详情页编辑也自然支持。

 ## 不需要做的

 - 不需要修改 `CreateIssueRequest` / `UpdateIssueRequest` 类型 —— 已有 `runtime_id` 字段。
 - 不需要修改 `squadLeaderRuntimeOptions` 的 React Query key 或 staleTime —— 已有合理配置。
 - 不需要修改 squad 的创建/编辑流程 —— 创建 squad 本来就不涉及运行时选择。
 - 不需要修改 issue 列表/缓存逻辑 —— `runtime_id` 是 per-run 字段，不持久化在 issue 行上。
 - 不需要修改可用的 squad API（list、create、update、delete）—— 仅需要修改 leader compatible runtimes 的 owner 逻辑。

 ## 测试计划

 ### 后端测试

 - 用户 A 创建 squad，队长 agent 的 provider 是 codex。
 - 用户 B 有 2 个在线 Codex 运行时，调用 `ListSquadLeaderCompatibleRuntimes` 返回 B 的 2 个运行时。
 - 用户 B 有 1 个在线 Codex 运行时，调用 `ListSquadLeaderCompatibleRuntimes` 返回 B 的 1 个运行时。
 - 用户 B 没有在线 Codex 运行时，调用 `ListSquadLeaderCompatibleRuntimes` 返回空列表。
 - 用户 B 只有离线 Codex 运行时，调用 `ListSquadLeaderCompatibleRuntimes` 返回空列表（只返回 online）。
 - 用户 B 有在线 Codex 运行时但不兼容队长 agent 的 provider，调用 `ListSquadLeaderCompatibleRuntimes` 返回空列表。
 - 用户 B 创建 issue 并指定 squad assignee + 自己的 runtime_id：task 使用该 runtime_id 创建。
 - 用户 B 创建 issue 并指定 squad assignee + 别人的 runtime_id：被后端拒绝（owner_id 校验不通过）。
 - 用户 B 创建 issue 并指定 squad assignee + 不传 runtime_id：走现有 task resolver 自动选择。

 ### 前端测试

 - Squad assignee 选择流程：
   - 0 个兼容运行时：点击 squad 后不应提交，显示错误提示。
   - 1 个兼容运行时：点击 squad 后自动选中运行时，提交中包含 `runtime_id`。
   - 2+ 个兼容运行时：点击 squad 后展开运行时选择子视图，用户选择后提交 `runtime_id`。
 - 运行时选择子视图：
   - 点击返回箭头回到 squad 列表。
   - 点击运行时条目后关闭 picker 并提交。
 - 创建 issue 和编辑 issue 两个场景都覆盖。

 ## 验证命令

 - `make sqlc`
 - `make test`
 - `pnpm typecheck`
 - `pnpm test`

 ## 风险

 - **向后兼容性**：已有 `ListSquadLeaderCompatibleRuntimes` API 的行为发生变化（从返回队长 owner 的运行时改为返回请求者自己的运行时）。需要确认是否有已部署的前端依赖旧行为。根据代码搜索，该 API 目前仅在前端的 `squadLeaderRuntimeOptions` 中使用，而该 hook 在当前代码库中未被任何组件引用（是死代码），所以旧行为无消费者，可以安全修改。
 - **squad task 入队路径**：目前 squad task 入队（`enqueueSquadLeaderTask`）没有利用前端传入的 runtime_id。如果修改后，已发版客户端（不传 runtime_id）的请求仍然走旧路径（task resolver），不会受影响。
 - **运行时兼容性匹配**：squad leader 的 agent 可能有 `runtime_profile_id`，不是所有 `provider` 匹配的运行时都兼容。必须确保后端 `ListUserCompatibleRuntimes` 查询同时检查 `profile_id` 和 `provider`。
 - **离线运行时**：用户可能有多个本地运行时但部分离线。API 只返回在线且兼容的运行时，避免用户选择了离线运行时导致 task 无法执行。
