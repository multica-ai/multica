# Spec：解除智能体与本地运行时所有权绑定

## 问题

当前智能体通过 `agent.runtime_id` 实际绑定到一条具体的 `agent_runtime` 记录。这个运行时通常由最早注册本地守护进程的成员创建。团队其他成员使用同一个工作区可见的智能体时，新 task 仍会排到创建者的运行时上，结果是在创建者的机器和 provider 凭据下执行，而不是使用触发者自己的本地运行时。

目标行为：

- 智能体仍然是工作区共享对象：名称、说明、instructions、skills、模型偏好、MCP 配置、环境变量元数据和可见性保持共享。
- 创建智能体时不绑定具体运行时；智能体只声明自己需要哪类运行时能力，例如 provider 或 custom runtime profile。
- 本地执行跟随用户：某个成员触发智能体时，只要该成员有兼容的本地运行时，平台就应把 task 路由到该成员自己的运行时。
- 运行时是严格私有资源：只有运行时所有者本人可以看见和调用；工作区 owner/admin 也不能看见、选择或调用其他成员的运行时。
- 运行日志可以按现有业务权限查看，但日志查看权不等于运行时可见权或调用权。
- 现有守护进程 claim 机制仍然可以使用具体的 task `runtime_id`；绑定关系应在 task 创建时决定，而不是永久存储为智能体的执行位置。

## 当前绑定点

- `server/migrations/004_agent_runtime_loop.up.sql` 将 `agent.runtime_id` 设为必填，并新增 `agent_task_queue.runtime_id`。
- `server/pkg/db/queries/agent.sql` 在 `CreateAgent` / `UpdateAgent` 中写入 `agent.runtime_id`，随后把它复制到 issue、mention、retry、quick-create 等 task。
- `server/pkg/db/queries/chat.sql` 从 `agent.runtime_id` 写入 `chat_session.runtime_id`，并用显式运行时创建 chat task。
- `server/pkg/db/queries/autopilot.sql` 用显式运行时创建自动化 task。
- `server/internal/service/task.go` 在 `agent.RuntimeID` 为空时拒绝入队，否则把 task 排到 `agent.RuntimeID`。
- `server/internal/service/agent_ready.go` 将 ready 定义为“智能体有绑定运行时，且该运行时在线”。
- `server/internal/service/task.go` 的守护进程 claim 路径已经按运行时工作：`ClaimTaskForRuntime` 会按 `agent_task_queue.runtime_id` 列出可 claim 的 task。
- `packages/views/agents/components/create-agent-dialog.tsx` 和 `runtime-picker.tsx` 把运行时选择作为智能体创建的一部分。

## 推荐方案

新增一个运行时解析层，建立下面的映射：

`智能体 + 触发用户 + 来源类型 -> 具体 runtime_id`

继续保持 `agent_task_queue.runtime_id` 非空。它仍然是持久执行记录和守护进程路由键。需要改变的是写入它的时机和来源：不再直接复制智能体创建者的运行时，而是在 task 入队时解析当前 task 应该使用的运行时。

第一阶段不要让新建智能体继续写入具体 `agent.runtime_id`。迁移后智能体保存 `runtime_provider` 和可选 `runtime_profile_id` 作为运行时能力要求；`agent.runtime_id` 仅作为旧数据和旧客户端兼容字段保留，不能再代表“这个智能体绑定到某台机器”。

这是最小且安全的改法。它保留当前守护进程架构和审计历史，同时解除共享智能体身份与本地机器所有权之间的耦合。

### 备选方案

1. **把所有运行时设为 public，让团队成员手动绑定或移动智能体。**
   实现成本低，但不能解决产品问题。智能体仍然同时只能有一个 active 运行时，某个用户移动后会影响其他用户。

2. **彻底移除 `agent.runtime_id`，按 provider/profile 动态 claim。**
   概念上更干净，但改动面太大。守护进程轮询、ready 判断、chat 恢复、用量归因、级联删除、运行时详情页和已安装客户端都假设 task 行上有具体运行时。

## 运行时解析规则

新增服务端 resolver，例如 `server/internal/service/runtime_resolver.go`。

输入：

- `workspace_id`
- `agent`
- 触发用户 ID，如果已知
- issue 创建/分配时用户显式选择的运行时 ID，如果有
- task 来源：issue 分配、mention、chat、quick-create、自动化、retry
- 可选的历史 task/session 数据，用于需要恢复会话的路径

输出：

- 具体的 `agent_runtime`
- 用于诊断和用户可见错误的 reason code

解析顺序：

1. 如果来源必须恢复已有 provider 会话，优先使用上一条 task 或 chat session 的运行时，前提是该运行时在线且仍可访问。这样保护 chat 连续性和 retry 语义。
2. 如果 issue 创建/分配入口传入了显式运行时 ID，先校验该运行时属于触发用户本人、在线、在同一工作区内，并且满足智能体的 `runtime_provider` / `runtime_profile_id` 能力要求；校验失败则拒绝入队。
3. 如果没有显式运行时 ID，则在同一工作区内查找触发用户自己的在线本地运行时，并要求它满足智能体运行时能力要求：
   - 如果智能体设置了 `runtime_profile_id`，匹配相同 `profile_id`。
   - 否则匹配相同 `provider`。
   - 如果存在多个候选，默认选择排序后的第一个，排序规则应稳定：在线、当前用户拥有、创建时间或名称。
4. 如果找不到触发用户自己的兼容本地运行时，则在入队前失败，并返回清晰原因，例如 `no_compatible_runtime_for_user`。

关键约束：没有兜底运行时。不要使用其他成员的 private 运行时、工作区 public 本地运行时、cloud 运行时或旧 `agent.runtime_id` 指向的运行时来替代触发用户的本地运行时。否则会重新引入原来的隐私和执行归属问题。

运行时本体是严格私有资源：只有运行时所有者本人可以看见和调用自己的运行时。工作区 owner/admin 也不能把别人的运行时用于 task，也不能在可选运行时列表里看到别人的运行时。已经产生的运行日志和 task 历史仍按现有 issue/chat/agent 权限展示；查看日志不等于获得运行时调用权。

## 数据模型

新增迁移，建议命名：

- `server/migrations/122_agent_runtime_resolution.up.sql`
- `server/migrations/122_agent_runtime_resolution.down.sql`

推荐 schema 变化：

- 新增 `agent.runtime_provider TEXT NOT NULL`，表示智能体需要的 provider 能力。
- 新增 `agent.runtime_profile_id UUID NULL REFERENCES runtime_profile(id) ON DELETE SET NULL`，表示智能体需要的 custom runtime profile；为空时按 `runtime_provider` 匹配 built-in 运行时。
- 从现有 `agent.runtime_id` 回填 `runtime_provider` 和 `runtime_profile_id`。
- 将 `agent.runtime_id` 改为可空，并保留为旧 API 和旧客户端兼容字段；新建智能体不再写入具体 `runtime_id`。
- 复用并扩展 `agent_task_queue.initiator_user_id`，让它覆盖所有用户触发的 task。该字段已由迁移 117 添加，且有意不加外键；本次把含义从“chat 触发者”扩展为“该 task 背后的人类请求者”。
- 为解析路径新增索引：
  - `agent_runtime(workspace_id, owner_id, status, provider) WHERE runtime_mode = 'local'`
  - `agent_runtime(workspace_id, owner_id, status, profile_id) WHERE runtime_mode = 'local' AND profile_id IS NOT NULL`

后续可选清理：

- 兼容窗口结束后，移除或隐藏 API 中的 `runtime_id` 兼容字段。
- 等所有客户端和 handler 不再读取它之后，再考虑删除 `agent.runtime_id`。

## 后端改动

### Queries

在 `server/pkg/db/queries/runtime.sql` 新增 resolver 需要的查询：

- `FindUserRuntimeByProvider`
- `FindUserRuntimeByProfile`
- `ListUserCompatibleRuntimes`

更新 `server/pkg/db/queries/agent.sql`：

- `CreateAgentTask`、`CreateQuickCreateTask` 和 retry 路径继续接收显式 `runtime_id`。
- `CreateAgent` / `UpdateAgent` 改为写入 `runtime_provider` 和 `runtime_profile_id`，不再要求或写入具体 `runtime_id`。
- 旧客户端如果仍提交 `runtime_id`，服务端只用它解析 provider/profile 并写入能力要求；不要把它作为新智能体的机器绑定。

更新 `server/pkg/db/queries/chat.sql`：

- `CreateChatSession` 不应再盲目从智能体复制 `runtime_id`。
- 创建 chat task 时接收 resolver 选出的运行时。
- `UpdateChatSessionSession` 可以在会话建立后继续固定 `runtime_id`。

更新 `server/pkg/db/queries/autopilot.sql`：

- 创建自动化 task 时接收 resolver 选出的运行时。
- 对没有人类触发者的 schedule/system 自动化，只能使用自动化所有者自己的兼容运行时配置；不得使用管理员、智能体创建者或其他成员的运行时，也不得作为普通 issue/chat/quick-create 路径的兜底。

### Services

把下面路径里直接使用 `agent.RuntimeID` 的逻辑替换为 resolver：

- `TaskService.enqueueIssueTask`
- `TaskService.enqueueMentionTask`
- `TaskService.EnqueueQuickCreateTask`
- `TaskService.EnqueueChatTask`
- `server/internal/service/autopilot.go` 中的自动化 dispatch 路径
- 外部触发适配器，尤其是 `server/internal/integrations/lark/dispatcher.go`

触发用户来源：

- issue 分配和手动 rerun：request user。用户 B 触发分配时总是使用 B 的运行时，而不是 issue 创建者、assignee 或智能体 owner 的运行时。
- mention/comment 触发：如果评论作者是成员，使用评论作者；否则在存在 request user 时回退到 request user
- chat：现有 `initiator_user_id`
- quick-create：`QuickCreateContext` 中已有 requester id
- schedule/system 自动化：没有 requester 时，使用自动化所有者自己的运行时；如果所有者没有兼容且在线的运行时，则失败，不跨用户兜底

Retry 行为：

- 自动 retry 失败 task 时，如果失败原因允许恢复会话，且父 task 的运行时在线，则保留父 task 的 `runtime_id`。
- 手动 rerun 且 `force_fresh_session=true` 时，按当前请求者重新解析运行时，让 rerun 可以移动到当前用户的本地运行时。
- chat 切换机器时应优先恢复旧运行时 session。只要旧 session 的运行时仍在线且可访问，后续消息继续使用旧运行时，而不是在当前用户机器上开启新 session。

Ready 判断：

- 用 source-aware readiness 替换当前 `AgentReadiness`：
  - 全局列表和详情页可以基于 provider/profile 能力要求显示“已配置”。
  - 分配、chat 等动作应检查“当前用户是否有兼容且在线的运行时”。

通知：

- `NotifyTaskEnqueued` 已经基于具体 task 运行时工作。继续通知解析后的 runtime id。

### 权限与隐私

- 成员可以触发工作区可见的智能体，但不会因此获得创建者 private 运行时的使用权。
- resolver 在用户触发路径中只能使用该成员自己的本地运行时。
- 如果该成员没有兼容且在线的本地运行时，入队失败；不使用工作区 public 本地运行时、cloud 运行时或智能体 owner/default 运行时兜底。
- 运行时不可被其他人调用，包括工作区 owner/admin。owner/admin 可以管理工作区和查看授权范围内的运行日志，但不能把别人的运行时作为执行资源。
- 运行时不可被其他人看见，包括工作区 owner/admin。运行时列表、issue 分配运行时选择器、智能体兼容运行时提示都只展示当前用户自己的本地运行时。
- 运行日志可以查看：task 历史、activity/history、运行输出、用量记录仍按 issue/chat/agent/workspace 的现有访问权限展示。日志里可展示实际执行过的 `runtime_id` 或脱敏运行时摘要，但不得泄露其他成员本地运行时的可调用入口、完整设备信息或选择项。

## API 与类型改动

新增向后兼容字段：

- Agent response：
  - 保留 `runtime_id` 兼容字段，但允许为空
  - 新增 `runtime_provider`
  - 新增 `runtime_profile_id`
- Create/update agent request：
  - 新接口接受 `runtime_provider` 和可选 `runtime_profile_id`
  - 兼容期接受旧 `runtime_id`，但只把它转换为 provider/profile 能力要求，不绑定机器
- issue 创建/分配请求：
  - 当 assignee 是智能体时，允许携带本次执行选择的 `runtime_id`
  - 服务端只把它作为 resolver 输入，不直接信任；必须校验它属于当前请求者本人、在线、同工作区且满足智能体 provider/profile 要求
  - 没有传入时，resolver 自动选择当前请求者第一个兼容本地运行时
- 运行时列表 / 运行时详情 API：
  - 普通用户、owner、admin 都只能列出和打开自己拥有的本地运行时
  - 如果已有运行时管理页需要保留管理员视角，只能展示聚合健康状态或脱敏审计信息，不能暴露可调用运行时对象，也不能提供“选择/调用他人运行时”的入口

需要更新：

- `packages/core/types/agent.ts`
- `packages/core/types/issue.ts` 或对应 issue request 类型
- `packages/core/api/schemas.ts`
- `packages/core/api/schema.test.ts` 中相关 malformed-response 测试

## 前端改动

智能体创建和编辑 UI 不应继续把运行时表达为“这个智能体永远运行在哪台机器上”。

推荐 UI 模型：

- 第一版立即重命名 UI 标签：创建/编辑智能体时选择“运行时类型”或“provider/profile”，不要再使用“运行时绑定”这类暗示固定机器归属的文案。
- 创建智能体时不选择具体运行时，也不显示“我的运行时”列表；只选择该智能体需要的 provider/profile 能力。
- issue 创建/分配时，如果 assignee 是智能体，应允许用户选择自己的兼容本地运行时；如果有多个兼容运行时，默认选择第一个。
- 智能体详情页展示：
  - 运行时类型 provider/profile
  - 当前用户的兼容运行时状态
  - activity/history 中每条 task 实际使用的运行时
- Runtime picker 如果继续复用，只能作为 provider/profile 选择器使用，不能显示为“绑定到某台本地机器”。
- issue 创建/分配运行时选择器只显示当前用户自己的兼容本地运行时。即使当前用户是 workspace owner/admin，也不显示其他成员的运行时。
- 运行日志、task 历史和 activity/history 可以展示执行结果与必要诊断信息；展示其他成员运行时相关信息时必须脱敏，不能让用户从日志跳转到可调用的他人运行时详情。

可能涉及文件：

- `packages/views/agents/components/create-agent-dialog.tsx`
- `packages/views/agents/components/runtime-picker.tsx`
- `packages/views/agents/components/agent-detail-inspector.tsx`
- `packages/views/agents/components/agent-detail-page.tsx`
- `packages/views/agents/components/agent-overview-pane.tsx`
- `packages/views/issues/` 下的新建/分配 issue 表单组件
- `packages/core/issues/` 下的 issue request 类型和 mutations
- `packages/core/agents/derive-presence.ts`
- `packages/views/locales/*/agents.json`
- `packages/views/locales/*/issues.json`
- `packages/views/locales/*/runtimes.json`

## 兼容计划

第一阶段：

- 新增 resolver、`runtime_provider` 和 `runtime_profile_id`。
- API 和 DB 继续保留 `runtime_id` 兼容字段，但新建智能体不再绑定具体运行时。
- 新 task 创建改为使用 resolver 选出的运行时。
- 第一版同步完成 UI 标签重命名，并在 issue 创建/分配流程加入当前用户本地运行时选择。
- 旧客户端继续可用，因为 `runtime_id` 仍然存在。

第二阶段：

- 补齐非核心入口的说明和空状态，解释“按用户本地运行时路由”的语义。
- 增强运行时兼容性提示和错误恢复路径。

第三阶段：

- 已安装客户端完成兼容窗口后，从 UI/API 移除 `runtime_id` 兼容字段。
- 评估是否删除 `agent.runtime_id`。

## 测试计划

后端测试：

- 用户 A 创建 Codex 类型智能体时不绑定具体运行时；agent 行只保存 Codex provider 能力。
- 用户 A 创建 custom profile 智能体时不绑定具体运行时；agent 行只保存对应 `runtime_profile_id`。
- 用户 B 有自己的在线 Codex 运行时；B 触发 chat 或 mention 后，task `runtime_id` 应为 B 的运行时。
- 用户 B 触发 issue 分配时，task `runtime_id` 应为 B 的运行时，而不是 issue 创建者、assignee 或智能体 owner 的运行时。
- 用户 B 创建/分配 issue 时有多个兼容本地运行时；UI 默认选择第一个，提交后后端写入该运行时。
- 用户 B 创建/分配 issue 时显式选择第二个兼容本地运行时；后端写入第二个运行时。
- 用户 B 有多个兼容本地运行时；后端自动选择排序后的第一个，并写入 task `runtime_id`。
- 用户 B 没有兼容运行时；入队失败，返回可产品化错误，且不创建 task 行。
- 用户 B 只有 private Claude 运行时，而智能体 `runtime_provider` 是 Codex；resolver 应拒绝，不混用 provider。
- 自定义运行时 profile：按 `profile_id` 匹配，而不是只按 provider 匹配。
- 自动 retry 在可安全恢复会话时保留父 task 运行时。
- 手动 rerun 解析到当前请求者的运行时。
- 用户 B 没有自己的兼容运行时，但工作区存在 public/cloud 兼容运行时；入队仍失败，不使用兜底。
- 永远不会选择其他用户拥有的 private 运行时。
- workspace owner/admin 不是运行时所有者时，不能在 resolver 中使用该运行时，也不能通过显式 `runtime_id` 绕过校验。
- workspace owner/admin 打开运行时列表时，看不到其他成员的本地运行时；但在有权限查看的 issue/chat/agent 历史中可以看到对应运行日志。

前端测试：

- 旧客户端仍能用兼容期的 `runtime_id` 提交创建请求；服务端只把它转换为 provider/profile 能力要求，不绑定机器。
- Runtime picker 文案明确表达它是默认/provider 选择，而不是固定机器绑定。
- 创建智能体表单不要求用户选择具体本地运行时。
- issue 创建/分配流程展示当前用户自己的兼容本地运行时列表；多个候选时默认选中第一个。
- workspace owner/admin 在运行时选择器和运行时列表里也只能看到自己的本地运行时。
- 运行日志页面仍可查看 task 输出和历史，但没有跳转或调用他人运行时的入口。
- 当创建者运行时离线、但当前用户有兼容运行时时，智能体 presence 不应被永久标记为离线。

验证命令：

- `make sqlc`
- `make test`
- `pnpm typecheck`
- `pnpm test`

## 风险

- 如果每条消息都解析到不同运行时，chat/session 连续性可能被破坏。缓解方式：chat session 一旦有运行时/session，就继续使用它；切换机器时也恢复旧运行时 session。只有旧运行时不可用时才失败并提示用户，而不是自动开启新 session。
- 模型和 thinking 选项是 provider 原生能力。缓解方式：resolver 要求兼容 provider/profile；智能体 provider/profile 变化时，UI 清空或校验 model/thinking。
- 自动化没有明确的“当前用户”。缓解方式：在产品进一步定义按触发人路由之前，schedule/system 工作只允许使用自动化所有者自己的兼容运行时；所有者没有可用运行时时失败，不能借用管理员、智能体创建者或其他成员的运行时。
- 运行时用量看板会从“智能体创建者消耗运行时”转向“请求者消耗运行时”。这是符合目标的变化，但发布说明需要明确说明。

## 已确认决策

- 用户 B 触发 issue 分配时，使用 B 的运行时。
- 没有兜底运行时；不使用工作区 public 本地运行时、cloud 运行时或智能体 owner/default 运行时兜底。
- chat 切换机器时恢复旧运行时 session。
- 第一版立即重命名 UI 标签。
- 使用推荐方案：task 保留具体运行时，按 task 动态解析。
- 创建智能体时不绑定具体运行时，只保存 provider/profile 能力要求。
- issue 创建/分配时允许用户选择自己的本地运行时；如果有多个兼容运行时，默认选择第一个。
- 运行时不允许被其他人调用，workspace owner/admin 也不例外。
- 运行时不允许被其他人看见，workspace owner/admin 也不例外。
- 运行日志可以按现有业务权限查看，但查看日志不授予运行时可见性或调用权。
