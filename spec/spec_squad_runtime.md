# Spec：新建 Issue 时 Squad 队长运行时选择

## 背景

参考 [spec.md](../spec.md) —— 运行时解绑方案已实现了核心机制：

- 新增 runtime_resolver.go，按 requester 动态解析运行时
- 新增 AgentOwnerID 字段，在 squad 场景下校验运行时归属
- 前端 assignee-picker.tsx 已具备 squad 运行时选择 UI

但当前实现中存在一处 **handler 与 resolver 不一致** 的缺陷，导致 squad 运行时选择功能在线上不可用。

## 问题描述

### 数据流

当用户 A 在 Issue 分配器中选择了 Squad（队长 = Agent，Agent 的 owner = 用户 B）：

1. 前端调用 ListSquadLeaderCompatibleRuntimes(squadId) 获取可选运行时列表
2. Handler 用 member.UserID（即用户 A）查询运行时 -> 返回用户 A 自己的运行时
3. 用户 A 选择其中一个运行时并提交
4. 后端 enqueueIssueTask -> resolveRuntimeForTask -> RuntimeResolver.Resolve()
5. Resolver 中 AgentOwnerID = agent.OwnerID（即用户 B）
6. Explicit choice 路径校验：runtime.OwnerID == AgentOwnerID -> 用户 A 的运行时属于 A，不等于 B -> 返回 ErrRuntimeNotFound

### 根因

ListSquadLeaderCompatibleRuntimes handler 用 member.UserID（提请求者）查询兼容运行时，但 resolver 的 explicit choice 校验路径使用 AgentOwnerID（Squad leader agent 的 owner）。两者不一致导致前端选中的运行时总被后端拒绝。

### 代码证据

**Handler**（server/internal/handler/squad.go）当前使用 member.UserID 查询运行时，而 **Resolver**（server/internal/service/runtime_resolver.go）使用 AgentOwnerID 做 explicit choice 校验，两者不一致。

**TaskService**（server/internal/service/task.go）的 resolveRuntimeForTask 始终将 AgentOwnerID 设为 agent.OwnerID。

## 设计意图分析

参考 spec.md 的已确认决策：

- 用户 B 触发 issue 分配时，总是使用 B 的运行时
- 新建 issue 时，如果选择了小队，则队长的运行时需显示让用户选择

决策意图：**Squad 场景下，issue 创建者选择的是队长（leader agent owner）的运行时**，而不是自己的运行时。

## 修复方案

### 方案 A（推荐）：修改 Handler

将 ListSquadLeaderCompatibleRuntimes 改为查询 leader agent owner 的运行时，而非 requester 的：

`go
// 修改前
OwnerID: member.UserID,

// 修改后
OwnerID: leaderAgent.OwnerID,
`

**优点**：
- 与 resolveRuntimeForTask 中 AgentOwnerID 保持一致
- 与现有 TestRuntimeResolverExplicitChoiceUsesAgentOwnerForSquad 测试预期一致
- 改动最小（仅改一行）

**缺点**：
- 需要前端也将该运行时列表视为队长机器的运行时而非我的运行时
- 与运行时私有性原则存在一定张力：用户 A 可以看到队长 B 的运行时列表

**运行时私有性的处理方式**：
- Squad 运行时选择是 **有意设计的例外**：当分配给 Squad 时，由队长执行任务
- 前端 UI 需明确说明这是队长的运行时，而非用户的私有运行时列表
- 仅 squad 场景有这个特权；其他场景严格遵循运行时私有性

### 方案 B：修改 Resolver

将 Resolver 中 squad 场景下的校验改为使用 RequesterUserID 而非 AgentOwnerID。

**优点**：
- 与运行时私有性原则完全一致：用户 A 始终使用自己的运行时

**缺点**：
- 改动影响面更大：Resolve 方法被 chat、mention、autopilot 等多条路径调用
- 不符合队长的运行时需显示让用户选择的产品决策
- AgentOwnerID 设计目的就是 squad 场景，去掉后该字段冗余

## 推荐方案 A 的详细变更清单

### 后端

1. **handler/squad.go** - ListSquadLeaderCompatibleRuntimes
   - 将 OwnerID: member.UserID 改为 OwnerID: leaderAgent.OwnerID
   - 更新注释：说明 squad 场景下显示的是 leader 的运行时

2. **service/runtime_resolver_test.go** - 新增测试用例
   - 确保 handler 返回 leader owner 的运行时后，resolver 能正确校验
   - 验证 requester A 提交 leader B 的运行时能通过校验

3. **handler/daemon.go** - project_resources 逻辑
   - squad 场景下，分配给 leader owner 的运行时后，filterProjectResourcesForRuntime 需确保 leader owner 的 daemon 能获取到对应的 local_directory（参考 bug_runtime_project_dir.md）

### 前端

1. **assignee-picker.tsx**
   - 无需大改：已通过 squadLeaderRuntimeOptions 调用 handler，handler 返回正确数据后即可
   - UI 文案可将运行时分组标题从运行时改为队长运行时以明确含义

## 验收条件

1. 用户 A 创建 Issue，选择 Squad，看到的是队长（leader agent owner）的运行时列表
2. 用户 A 选择一个运行时（或自动选择唯一可用的），提交后 task 创建成功
3. Task 的 runtime_id 是队长 B 的运行时
4. 队长 B 的 daemon 能正常 claim 该 task 并执行
5. 非 squad 场景（普通 agent）行为不变
6. 单元测试通过

