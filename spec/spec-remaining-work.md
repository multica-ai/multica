# Spec：运行时解绑 — 剩余工作汇总

## 状态总览

| 模块 | 状态 | 说明 |
|------|------|------|
| RuntimeResolver | 已完成 | runtime_resolver.go - explicit/auto 路径均按请求者运行时解析，保留 capability 匹配 |
| DB 查询 (runtime.sql) | 已完成 | ListUserCompatibleRuntimes、FindUserRuntimeByProvider/Profile 就绪 |
| 迁移 (122_agent_runtime_resolution) | 已完成 | agent.runtime_provider、agent.runtime_profile_id、可空 runtime_id |
| 后端 task 入队使用 resolver | 已完成 | 所有入队路径（issue/mention/chat/quick-create/autopilot/retry）都走 resolver |
| Bug 3：Chat/Autopilot project resources | 已修复 | daemon.go 的 Chat/Autopilot/QuickCreate 分支都加载了 project resources |
| 前端 agent 创建：不绑定运行时 | 已完成 | 只选 provider/profile，不再选具体 runtime |
| 前端 issue assignee：agent 运行时选择 | 已完成 | agent 路径有运行时选择子视图 |
| Bug 1：ListSquadLeaderCompatibleRuntimes | 已修复 | 已查询当前请求用户的兼容运行时，不再查询 agent owner 的运行时 |
| Bug 1a：resolveRuntimeForTask AgentOwnerID 策略 | 已修复 | 已移除 AgentOwnerID override，explicit runtime choice 始终按 RequesterUserID 校验 |
| 前端 assignee：squad 运行时选择 | 已验证 | create/quick-create/assignee picker 已使用 squad leader runtime options |
| Bug 2：daemon_id 不匹配 | 已修复 | daemon 解析 local_directory 时不再按 daemon_id 过滤，仅将 daemon_id 作为元数据 |
| 运行时私有性权限校验 | 已验证 | runtime list/use/update/delete/daemon access 已有 owner-only 测试覆盖 |

---

## Bug 1：ListSquadLeaderCompatibleRuntimes & AgentOwnerID 策略

### 修复前行为（已修复）

**文件：** server/internal/handler/squad.go:265-315

ListSquadLeaderCompatibleRuntimes 修复前查询的 OwnerID 是 leader.OwnerID（小队长智能体的创建者），而非当前请求用户。

```go
runtimes, err := h.Queries.ListAgentRuntimesByOwner(r.Context(), db.ListAgentRuntimesByOwnerParams{
    WorkspaceID: squad.WorkspaceID,
    OwnerID:     leader.OwnerID,  // 错误：应该用请求用户
})
```

**文件：** server/internal/service/task.go:142-155

resolveRuntimeForTask 修复前在 explicit choice 路径把 agent.OwnerID 作为 AgentOwnerID 传入：

```go
return NewRuntimeResolver(s.Queries).Resolve(ctx, RuntimeResolveInput{
    RequesterUserID:     requesterID,
    AgentOwnerID:        agent.OwnerID,  // 问题根源
    ExplicitRuntimeID:   selectedRuntimeID,
    AllowExplicitChoice: selectedRuntimeID.Valid,
})
```

**触发流程（用户 A 选择小队 S，leader agent L）：**

1. 用户 A 在 UI 选择小队 S，squadLeaderRuntimeOptions 应返回 A 的运行时
2. A 选择自己的运行时（runtime_1，owner=A），前端提交 runtime_id=runtime_1
3. 后端 resolveRuntimeForTask 传入：
   - RequesterUserID = A
   - AgentOwnerID = L.OwnerID（假设为 O，L 的创建者）
   - ExplicitRuntimeID = runtime_1（owner=A）
4. Resolver explicit choice 路径：
   - expectedOwner = AgentOwnerID = O
   - runtime_1.OwnerID = A != O -> ErrRuntimeNotFound

### 修复方案 A（推荐）

**原则：** 在 squad 场景中，请求者 A 使用 A 自己的运行时，而非小队长的运行时（符合运行时私有性设计）。

#### 修复 1：ListSquadLeaderCompatibleRuntimes

改为查询当前请求用户的兼容运行时，复用已有的 ListUserCompatibleRuntimes SQL 查询：

```go
func (h *Handler) ListSquadLeaderCompatibleRuntimes(w http.ResponseWriter, r *http.Request) {
    squad, _, ok := h.loadSquadInWorkspace(w, r)
    if !ok { return }

    leader, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
        ID:          squad.LeaderID,
        WorkspaceID: squad.WorkspaceID,
    })
    if err != nil {
        writeError(w, http.StatusNotFound, "squad leader agent not found")
        return
    }

    requesterID := requestUserID(r)

    runtimes, err := h.Queries.ListUserCompatibleRuntimes(r.Context(), db.ListUserCompatibleRuntimesParams{
        WorkspaceID: squad.WorkspaceID,
        OwnerID:     requesterID,
        ProfileID:   leader.RuntimeProfileID,
        Provider:    leader.RuntimeProvider,
    })
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to list runtimes")
        return
    }

    result := make([]RuntimeBriefResponse, 0, len(runtimes))
    for _, rt := range runtimes {
        result = append(result, RuntimeBriefResponse{
            ID:   uuidToString(rt.ID),
            Name: rt.Name,
        })
    }
    // 移除旧的 manual 过滤逻辑（ListUserCompatibleRuntimes SQL 已做）
    writeJSON(w, http.StatusOK, result)
}
```

#### 修复 2：resolveRuntimeForTask AgentOwnerID

explicit choice 路径必须校验运行时属于请求者（A），而非 agent owner（O）。

```go
func (s *TaskService) resolveRuntimeForTask(ctx context.Context, workspaceID pgtype.UUID, agent db.Agent, requesterID pgtype.UUID, selectedRuntimeID pgtype.UUID) (db.AgentRuntime, error) {
    resolvedAgent, err := s.agentWithRuntimeCapability(ctx, agent)
    if err != nil {
        return db.AgentRuntime{}, err
    }
    return NewRuntimeResolver(s.Queries).Resolve(ctx, RuntimeResolveInput{
        WorkspaceID:         workspaceID,
        Agent:               resolvedAgent,
        RequesterUserID:     requesterID,
        ExplicitRuntimeID:   selectedRuntimeID,
        AllowExplicitChoice: selectedRuntimeID.Valid,
    })
}
```

---

## Bug 2：daemon_id 不匹配导致 local_directory 不可用（P2）

### 当前行为

findLocalDirectoryAssignment（server/internal/daemon/local_directory.go:56-85）按 daemon_id 过滤，当 task 被另一个 daemon（不同 daemon_id）claim 时，local_directory 资源被跳过。

### 修复方案

已实现修复：移除 daemon_id 过滤，由 daemon 自行管理其本地目录锁定；同时项目资源层限制每个 project 只有一个 local_directory，避免 claim 响应带多条本地目录导致 daemon 无法确定工作目录。

```go
func findLocalDirectoryAssignment(resources []ProjectResourceData) (*localDirectoryAssignment, error) {
    for _, ref := range resources {
        if ref.ResourceType != "local_directory" {
            continue
        }
        // 不再按 daemon_id 过滤，daemon_id 仅作为资源元数据保留
        // ...
    }
}
```

**P2 优先级：** 此 Bug 属于边缘场景（daemon 重启/变更后），非核心功能阻塞项。

---

## 前端：squad 运行时选择（已验证）

需确保 assignee-picker.tsx 中点击 squad 后的完整流程：

```tsx
case "squad": {
    const { data: runtimes } = squadLeaderRuntimeOptions(wsId, s.id);
    if (!runtimes || runtimes.length === 0) {
        showError("你没有兼容小队长智能体的本地运行时");
        return;
    }
    if (runtimes.length === 1) {
        onUpdate({ assignee_type: "squad", assignee_id: s.id, runtime_id: runtimes[0].id });
    } else {
        showRuntimePicker(runtimes, (selected) => {
            onUpdate({ assignee_type: "squad", assignee_id: s.id, runtime_id: selected.id });
        });
    }
}
```

---

## 工作顺序与优先级

| 优先级 | 任务 | 文件 | 估算 |
|--------|------|------|------|
| P0 | 修复 ListSquadLeaderCompatibleRuntimes | server/internal/handler/squad.go | 已完成 |
| P0 | 修复 resolveRuntimeForTask AgentOwnerID | server/internal/service/task.go | 已完成 |
| P1 | 验证/修复前端 squad 运行时选择 | packages/views/issues/components/pickers/assignee-picker.tsx | 已验证 |
| P1 | 编写/更新后端测试 | server/internal/.../*_test.go | 已完成 |
| P2 | 修复 Bug 2 (daemon_id) | server/internal/daemon/local_directory.go | 已完成 |
| P2 | 运行时私有性权限审计 | 多个文件 | 已验证 |

## 验证命令

```bash
make sqlc && make test          # Go 后端
pnpm typecheck && pnpm test     # TypeScript 前端
make check                      # 完整验证
```
