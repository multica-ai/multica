 # Spec：新建 Issue 时 Squad 分配路径的运行时选择器
 
 ## 背景
 
 参考 `spec.md`（解除智能体与本地运行时所有权绑定），该 spec 确立了以下核心原则：
 
 - **智能体不绑定具体运行时**：创建智能体时只声明 `runtime_provider` / `runtime_profile_id` 能力要求
 - **本地执行跟随用户**：谁触发，就用谁自己的本地运行时执行
 - **运行时严格私有**：只有运行时 `owner_id` 本人可以看见、选择、调用；workspace owner/admin 也不例外
 - **没有兜底运行时**：找不到当前用户兼容运行时则入队失败，不使用他人运行时、workspace public 运行时或 cloud 运行时
 - **运行时日志与运行时权限分离**：日志可按业务权限查看，但不等于运行时可见/调用权
 
 在此框架下，`spec.md` 已规定 issue 创建/分配时若 assignee 为 agent，前端应展示运行时选择器，让用户从自己拥有的兼容运行时中选一个。
 
 **本 spec 补全 squad 分配路径**：当 assignee 为 squad（小队）时，后端会将任务实际路由到 squad 的 leader agent。此时前端同样需要展示运行时选择器——让当前用户选择"用我的哪个兼容运行时来执行该 leader agent 的任务"。
 
 ## 问题
 
 当前 `create-issue.tsx` 中：
 
 | 场景 | 运行时选择器 | 原因 |
 | --- | --- | --- |
 | assignee = agent | 显示 | `selectedAgent` 有值 → `compatibleRuntimes` 非空 → 下拉框渲染 |
 | assignee = squad | **不显示** | `selectedAgent` 为 undefined（仅 `assigneeType === "agent"` 时计算） |
 
 后端 `EnqueueTaskForSquadLeaderByRequesterWithRuntime` 已经接受 `selectedRuntimeID` 并走完整的 owner 校验 + 兼容性匹配链路，**后端不需要改动**。缺口纯粹在前端。
 
 ## 目标行为
 
 1. 用户在创建 issue 表单中选择一个小队作为 assignee
 2. 前端自动解析该小队的 leader agent（通过 `squad.leader_id`）
 3. 前端计算：当前用户拥有哪些**在线、且与 leader agent 的 `runtime_provider` / `runtime_profile_id` 兼容**的本地运行时
 4. 若有兼容运行时，展示运行时下拉选择器（与 agent assignee 路径视觉一致）
    - 多个时默认选中排序后的第一个（在线优先 → 创建时间 → 名称）
 5. 若用户没有任何兼容运行时，提交按钮禁用，并给出提示
 6. 提交时将选中的 `runtime_id` 写入 `CreateIssueRequest`
 
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
 
 ## 实现方案
 
 ### 方案：扩展 `create-issue.tsx` 中运行时选择器以覆盖 squad
 
 沿袭 agent assignee 路径已有的运行时选择器模式，最小化改动。
 
 #### 前端改动清单
 
 **`packages/views/modals/create-issue.tsx`**：
 
 1. **新增 `selectedAgentForRuntime` 计算属性**
    - `assigneeType === "agent"` 时：从 agents 列表中按 `assigneeId` 查找
    - `assigneeType === "squad"` 时：从 squad 列表中按 `assigneeId` 查找 squad，取其 `leader_id`，再从 agents 列表中查找该 agent
    - 其他情况：undefined
 
 2. **修改 `compatibleRuntimes` 依赖**
    - 从依赖 `selectedAgent` 改为依赖 `selectedAgentForRuntime`
 
 3. **修改运行时下拉框渲染条件**
    - 从 `assigneeType === "agent" && compatibleRuntimes.length > 0`
    - 改为 `(assigneeType === "agent" || assigneeType === "squad") && compatibleRuntimes.length > 0`
 
 4. **修改 `selectedRuntimeId` 清空条件**
    - 从 `assigneeType !== "agent"`
    - 改为 `assigneeType !== "agent" && assigneeType !== "squad"`
 
 5. **修改提交时 `runtime_id` 写入条件**
    - 从 `assigneeType === "agent" && selectedRuntimeId`
    - 改为 `(assigneeType === "agent" || assigneeType === "squad") && selectedRuntimeId`
 
 6. **扩展 `manualCreateSubmitDisabled`**
    - squad 路径下同样要求 `selectedRuntimeId` 非空才能提交
 
 7. **新增 squad 列表数据注入**
    - 在 `ManualCreatePanel` 中新增 `useSquadList` 查询（或复用 `AssigneePicker` 已有的 squad 缓存），使组件在 `assigneeType === "squad"` 时能读取 `squad.leader_id`
 
 **`packages/core/types/api.ts`**：
 
 8. **更新 `CreateIssueRequest.runtime_id` 注释**
    - 从 `// 仅用于 agent assignee` 扩展为 `// 用于 agent / squad assignee（squad 路径下按 leader agent 解析）`
 
 #### 改动文件汇总
 
 | 文件 | 改动性质 | 行数估计 |
 | --- | --- | --- |
 | `packages/views/modals/create-issue.tsx` | 逻辑扩展 | ~15 行 |
 | `packages/core/types/api.ts` | 注释更新 | 1 行 |
 
 #### 后端：无需改动
 
 `server/internal/service/issue.go` 中的 `enqueueSquadLeaderTask` → `EnqueueTaskForSquadLeaderByRequesterWithRuntime` → `enqueueMentionTask` → `resolveRuntimeForTask` 链路已完整支持 `selectedRuntimeID` 的传入、owner 校验和兼容性匹配。
 
 ## 数据流
 
 ```
 用户选择 Squad assignee
        │
        ▼
 前端解析: squad.leader_id → leaderAgent
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
 
 ## 测试计划
 
 ### 前端测试（推荐新增）
 
 1. 用户选择 squad，当前用户有兼容 leader agent 的在线运行时 → 下拉框出现，默认选中第一个
 2. 用户选择 squad，当前用户无兼容运行时 → 下拉框不出现，提交按钮禁用
 3. 用户选择 squad，手动选第二个兼容运行时 → 提交请求中 `runtime_id` 为所选值
 4. 用户从 squad 切换为 agent assignee → 运行时选择器正常切换，不残留 squad 路径的选中值
 5. 用户从 squad 切换为 member assignee → 运行时选择器消失，`runtime_id` 清空
 6. `manualCreateSubmitDisabled` 在 squad 路径下与 agent 路径行为一致（无兼容运行时禁用提交）
 
 ### 后端测试
 
 已有覆盖，无需新增：
 - `squad_assign_trigger_test.go`：`TestCreateIssueAssignedToSquadEnqueuesLeader` 已验证 squad 分配后 task 入队
 - 相关 task 测试：覆盖 `resolveRuntimeForTask` 的兼容性校验和 owner 校验
 
 ## 兼容性
 
 - 不改变后端 API
 - 不改变 squad 数据模型
 - 不改变运行时隐私模型
 - 现有 agent assignee 路径的运行时选择器行为完全不受影响
 
 ## 风险
 
 - **TanStack Query 缓存**：`AssigneePicker` 打开时已 fetch squad 数据，TanStack Query 复用缓存，`ManualCreatePanel` 新增 squad 查询不会产生额外网络请求
 - **Leader agent 不可用**（已归档/被移除）：`agents.find` 返回 undefined，`selectedAgentForRuntime` 为 undefined → `compatibleRuntimes` 为空 → 下拉框不出现，与 agent 路径中 agent 不可用的行为一致
 - **Leader agent 的 provider/profile 变更**：用户已选择 squad 后若 leader agent 配置变更，`compatibleRuntimes` 会自动重新计算，下拉框选项随之更新
