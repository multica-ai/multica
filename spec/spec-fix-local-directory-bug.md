# Spec：运行时未按项目本地目录执行代码编写任务的问题分析与修复

## 问题现象

用户反馈：在项目中配置了 local_directory 资源（将智能体绑定到本地项目目录），但智能体执行代码编写任务时，**没有使用该本地目录**，而是使用了普通的 git worktree 临时目录。结果是智能体看不到用户本地的代码改动、分支和依赖文件，代码编写效果不符合预期。

## 链路追踪

全链路从「用户触发任务」到「daemon 使用本地目录执行」，关键路径如下：

`
用户触发任务 (issue创建/chat/mention)
  → Handler 调用 TaskService.EnqueueTaskForIssueByRequester()
    → TaskService.resolveRuntimeForTask()
      → RuntimeResolver.Resolve()  ← 解析出具体的 runtime_id
    → 写入 agent_task_queue (带有 runtime_id)
  → Daemon 轮询: ClaimTaskForRuntime()
    → Handler.ClaimTaskByRuntime()
      → 加载 issue 的 project resources (含 local_directory)
      → 构造 TaskResponse.ProjectResources
    → Daemon 接收 TaskResponse
      → Daemon 调用 findLocalDirectoryAssignment()
        → 匹配 local_directory 资源中的 daemon_id
        → 找到本地目录后锁定并执行
`

经全链路分析，定位到 **3 个独立 Bug**，任何一个都可能导致运行时无法正确使用项目本地目录。

---

## Bug 1（P0）：Squad（小队）场景下 RuntimeResolver 的 owner 校验过于严格

### 位置

server/internal/service/runtime_resolver.go:72-75

`go
if rt.OwnerID != input.RequesterUserID {
    return db.AgentRuntime{}, ErrRuntimeNotFound
}
`

### 触发场景

1. 用户 A 创建 issue，assignee 设置为小队（squad leader = 智能体 B）
2. 创建时，用户 A 在「运行时选择器」中选择了小队队长 B 的运行时
3. UI 将 B 的运行时 ID 作为 selectedRuntimeID 提交
4. Handler 调用 preflightSquadLeaderRuntime → ResolveRuntimeForAgentByRequester
5. Resolver 进入 explicit choice 路径（AllowExplicitChoice=true）
6. **校验 
t.OwnerID != input.RequesterUserID** → B 的运行时 OwnerID=B，但 RequesterUserID=A → **失败**

### 后果

- 返回 ErrRuntimeNotFound
- 前端收到 gent_unavailable 错误，丢弃用户选择的运行时
- Issue 创建失败或静默退到 fallback（如果没有 fallback 则阻塞）

### 调用栈

`
Handler.createIssue()
  → preflightSquadLeaderRuntime(ctx, issue, requesterID=A, selectedRuntimeID=B_runtime)
    → TaskService.ResolveRuntimeForAgentByRequester(workspaceID, squad.LeaderID=B, requesterID=A, selectedRuntimeID=B_runtime)
      → resolveRuntimeForTask(agent=B, requesterID=A, selectedRuntimeID=B_runtime)
        → RuntimeResolver.Resolve(RuntimeResolveInput{
             RequesterUserID: A,
             ExplicitRuntimeID: B_runtime,
             AllowExplicitChoice: true
           })
          → rt.OwnerID (B) != RequesterUserID (A) → ErrRuntimeNotFound ✗
`

### 修复方案

**方案 A（推荐）**：在 RuntimeResolveInput 中增加 AgentOwnerID 字段

`go
type RuntimeResolveInput struct {
    WorkspaceID         pgtype.UUID
    Agent               db.Agent
    RequesterUserID     pgtype.UUID
    ExplicitRuntimeID   pgtype.UUID
    AllowExplicitChoice bool
    AgentOwnerID        pgtype.UUID  // 新增：智能体所有者的 user_id
}
`

修改 explicit choice 校验逻辑：

`go
if input.AllowExplicitChoice && input.ExplicitRuntimeID.Valid {
    rt, err := r.getRuntime(ctx, input.ExplicitRuntimeID)
    // ...
    // 优先比较 AgentOwnerID（小队队长场景），再回退到 RequesterUserID
    ownerToCheck := input.RequesterUserID
    if input.AgentOwnerID.Valid {
        ownerToCheck = input.AgentOwnerID
    }
    if rt.OwnerID != ownerToCheck {
        return db.AgentRuntime{}, ErrRuntimeNotFound
    }
    // ...
}
`

**涉及的修改点**：
1. 
untime_resolver.go — 新增 AgentOwnerID 字段 + 修改 owner 校验逻辑
2. 	ask.go — 
esolveRuntimeForTask 接收并传递 AgentOwnerID
3. 	ask.go — ResolveRuntimeForAgentByRequester 接收并传递 AgentOwnerID
4. issue.go — preflightSquadLeaderRuntime 传入 squad leader agent 的 owner ID
5. 
untime_resolver_test.go — 新增 squad 场景测试用例

---

## Bug 2（P2）：daemon_id 不匹配导致 local_directory 无法解析

### 位置

server/internal/daemon/local_directory.go:56-95

`go
func findLocalDirectoryAssignment(resources []ProjectResourceData, daemonID string) (*localDirectoryAssignment, error) {
    // ...
    if ref.DaemonID != daemonID {
        // 跳过——其他 daemon 的 local_directory
        continue
    }
    // ...
}
`

### 触发场景

1. 用户在小队队长 B 的机器上配置了项目本地目录（local_directory 资源绑定到 B 的 daemon）
2. 用户 A 创建 issue 并正确解析到 B 的运行时
3. Daemon B 的 daemonID 与 local_directory 资源中的 daemon_id 匹配
4. **正常场景下应该匹配成功**

但以下场景会触发此 Bug：

- **场景 a**：运行时跨 daemon 复用——某个 daemon 可能会 claim 另一个 daemon 创建的运行时关联的任务（尽管任务有 
untime_id，但 claim 的 daemon 身份不同）
- **场景 b**：local_directory 资源创建时的 daemon_id 与当前 claim 任务的 daemon 的 daemon_id 不同（例如用户重新注册了运行时，daemon_id 变化了）
- **场景 c**：local_directory 只被一个 daemon 配置，但任务被另一个 daemon claim（如多个 daemon 共享同一个运行时模式）

### 后果

- local_directory 资源 silently skipped
- indLocalDirectoryAssignment 返回 nil（无错误）
- Daemon 退到普通 worktree 路径
- 代码编写任务在临时目录执行，看不到用户本地代码

### 修复方案

**方案 A（推荐）**：在 task 行上持久化 daemon_id，让 local_directory 按 task 的 daemon_id 查找

`go
// 在 agent_task_queue 表新增 daemon_id 字段
// 在 ClaimTaskForRuntime 返回时记录实际 claim 的 daemon_id
// findLocalDirectoryAssignment 改为接收 taskDaemonID 和当前 daemonID
`

**方案 B（轻量）**：放宽 daemon_id 匹配条件

`go
if ref.DaemonID != daemonID {
    // 如果 local_directory 的 daemon_id 为空或匹配失败，
    // 尝试匹配 label/owner 作为补充条件
    continue  // 保持现有行为，但增加 fallback 逻辑
}
`

**优先推荐方案 A**，因为方案 B 可能引入安全风险（错误地使用其他 daemon 的本地目录）。

---

## Bug 3（P1）：Chat/Autopilot 任务没有携带 project resources

### 位置

server/internal/handler/daemon.go:1530-1600

`go
// Chat task: populate workspace/session info from the chat_session table.
if task.ChatSessionID.Valid {
    if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
        resp.WorkspaceID = uuidToString(cs.WorkspaceID)
        // ...
        // ⚠ 只加载了 workspace repos，没有加载 project resources
        // 没有调用 listProjectResourcesForProject
    }
}

// Autopilot run_only task
if task.AutopilotRunID.Valid {
    // ⚠ 同样没有加载 project resources
}
`

### 触发场景

1. 用户在 Chat 中要求智能体执行代码编写任务
2. Chat 消息触发的 task 没有 issue 关联（或关联的 issue 未在 ClaimTaskByRuntime 中被解析 project resources）
3. 即使 Chat session 被反查到关联的 issue（通过 chat_session.IssueID），也没有加载该 issue 的 project 下的 local_directory
4. Daemon 收到的 TaskResponse.ProjectResources 为空

### 后果

- Daemon 无法找到 local_directory 映射
- 回退到 git worktree 模式
- Chat 中的代码编写任务不使用用户配置的本地项目目录

### 修复方案

**方案 A（推荐）**：在 Chat 和 Autopilot 分支中，反查 issue → project → project resources

`go
if task.ChatSessionID.Valid {
    if cs, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID); err == nil {
        // ... 现有逻辑 ...

        // 新增：如果 chat session 关联了 issue，加载其 project resources
        if cs.IssueID.Valid {
            if issue, err := h.Queries.GetIssue(r.Context(), cs.IssueID); err == nil && issue.ProjectID.Valid {
                // 复用现有 project resources 加载逻辑（与 issue task 相同）
                if rows := h.listProjectResourcesForProject(r.Context(), issue.ProjectID); len(rows) > 0 {
                    // ... 构建 ProjectResources 和 projectRepos ...
                }
            }
        }
    }
}
`

**涉及的修改点**：
1. daemon.go — 在 Chat 分支加入 project resources 加载逻辑
2. daemon.go — 在 Autopilot 分支加入 project resources 加载逻辑（如 autopilot run 关联了 issue）
3. daemon_test.go — 新增 Chat/autopilot 带 project resources 的测试用例

---

## 三个 Bug 的关系图

`
用户 A 创建 issue，选择小队队长 B 的运行时
  │
  ├── Bug 1 ──→ RuntimeResolver 拒绝（OwnerID校验失败）
  │              │
  │              └──→ Issue 创建失败 / 代理不可用错误
  │
  └── 假设 Bug 1 已修复 ──→ Task 正确入队到 B 的运行时
                            │
                            ├── Bug 2 ──→ daemon_id 不匹配
                            │            │
                            │            └──→ local_directory 被跳过
                            │
                            └── 假设 Bug 2 已修复 ──→ Daemon 正确找到 local_directory
                                                      │
                                                      └── Chat 发起的任务 ──→ Bug 3 ──→ 没有 project resources
                                                                                       │
                                                                                       └──→ local_directory 为空
`

**三个 Bug 独立存在**，修复顺序：**Bug 1 (P0) → Bug 3 (P1) → Bug 2 (P2)**

- Bug 1 是 squad 场景下最直接的阻塞，用户根本无法创建/分配 issue 到 squad
- Bug 3 影响 Chat 代码编写体验，频率较高
- Bug 2 属于边缘场景（daemon 变更后触发）

---

## 验收测试用例矩阵

| # | 场景 | 预期 | 覆盖 Bug |
|---|------|------|----------|
| 1 | 用户 A 创建 issue，assignee=小队（leader=B），选择 B 的运行时，提交成功 | Issue 创建成功，task 携带 B 的 runtime_id | Bug 1 |
| 2 | 用户 A 创建 issue，没有选择运行时，auto-pick 找到 A 的第一个兼容运行时 | Issue 创建成功，task 携带 A 的 runtime_id | Bug 1 |
| 3 | 用户 B 触发 squad leader 任务，B 有 local_directory 在 project 中 | Daemon 正确解析 local_directory 并锁定路径 | Bug 2 |
| 4 | Chat 中提及 issue 触发代码编写任务，该 issue 关联了 project 且有 local_directory | Daemon 收到 ProjectResources 包含 local_directory | Bug 3 |
| 5 | 用户通过 Chat 直接提问（无 issue 关联），发起代码编写任务 | Daemon 按正常 worktree 路径执行（不阻塞） | Bug 3 |
| 6 | Squad leader runtime 下线后，用户 B 重新创建新运行时，原有 local_directory daemon_id 变更 | Daemon 仍能匹配 local_directory（方案 A 修复后） | Bug 2 |
| 7 | 复制粘贴已有测试：
untime_resolver_test.go 中的 squad 场景 | 测试通过 | Bug 1 |
| 8 | 复制粘贴已有测试：daemon_test.go 中的 Chat with project resources 场景 | 测试通过 | Bug 3 |

## 验证命令

`ash
make sqlc           # 重新生成 Go 查询代码
make test           # Go 后端测试
pnpm typecheck      # TypeScript 类型检查
pnpm test           # 前端测试
`

---

## 风险评估

| 风险 | 影响 | 缓解 |
|------|------|------|
| Bug 1 修复后，Resolver 可能错误地把非 squad 场景的 runtime 也允许跨用户使用 | 安全风险 | 新增 AgentOwnerID 字段，仅在 squad leader 场景设置；非 squad 场景该字段为空，使用原有的 RequesterUserID |
| Bug 3 修复后，Chat 任务多加载 project resources，可能增加 DB 查询和响应大小 | 性能影响极低 | 查询有索引，Chat 场景本身低频 |
| Bug 2 放宽 daemon_id 匹配可能误匹配 | 安全风险 | 优先使用方案 A（持久化 daemon_id 到 task 行），不放宽匹配条件 |
