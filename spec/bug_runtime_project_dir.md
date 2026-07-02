# Bug：运行时未按照项目的本地目录执行代码编写任务

## 问题描述

在运行时解绑方案生效后（runtime 按 requester 动态解析），某些场景下 agent 执行任务时未使用项目配置的 local_directory，而是回退到默认的 workspaces 根目录执行代码编写任务。

## 根因分析

### 数据流

1. 用户 A 创建的 Task 被 resolver 路由到用户 B 的运行时（daemon ID = D2）
2. 后端处理 task 时，调用 filterProjectResourcesForRuntime 过滤项目资源
3. 该函数检查 local_directory 资源的 resource_ref.daemon_id 是否等于当前运行时（B）的 daemon_id
4. 由于 local_directory 的 daemon_id 匹配的是创建者 A 的 daemon（D1），而非 B 的 daemon（D2）
5. local_directory 资源被 **过滤掉**，返回空列表
6. Daemon 的 findLocalDirectoryAssignment 收到空列表 -> 返回 nil
7. Task 执行在默认的 workspaces root，而非项目配置的本地目录

### 代码定位

**关键函数**：server/internal/handler/daemon.go 中的 filterProjectResourcesForRuntime

该函数对 local_directory 类型的 project_resource 检查 resource_ref 中的 daemon_id 是否与当前 runtime 的 daemon_id 匹配。当不匹配则过滤掉该资源。

**调用链路**：

1. enqueueIssueTask / dispatch
2. handler 处理 GetIssueDetail / GetProjectResources
3. filterProjectResourcesForRuntime(r.Context(), projectID, runtime)
4. daemon_id 不匹配 -> local_directory 被过滤
5. daemon claim task
6. findLocalDirectoryAssignment(resources, daemonID)
7. resources 为空 -> return nil
8. task 在默认 workspaces root 执行

### 根本原因

filterProjectResourcesForRuntime 的设计初衷是防止一个 daemon 获取另一个 daemon 的 local_directory。但在运行时解绑后，同一个项目的 local_directory 资源可能被多个用户的 daemon 共享（项目属于 workspace，local_directory 是项目级别的资源，而非用户级别的）。当任务被路由到另一个用户的运行时执行时，该运行时对应的 daemon 就无法获取到项目的 local_directory 资源。

## 影响范围

| 场景 | 是否受影响 | 说明 |
|------|-----------|------|
| 同一用户自己触发的 task | 不受影响 | daemon_id 一致，local_directory 正常 |
| 不同用户触发、但 B 也有该项目的 local_directory | 不受影响 | B 的 daemon 有自己的 local_directory 记录 |
| **不同用户触发、仅 A 配置了 local_directory** | **受影响** | B 的 daemon 拿不到 A 配置的 local_directory |
| Squad 场景：A 选队长 B 的运行时 | **受影响** | A 的项目配置了 local_directory，B 的 daemon 拿不到 |

## 修复方案

### 方案 A（推荐）：放宽 daemon_id 过滤条件

将 filterProjectResourcesForRuntime 对 local_directory 的过滤从严格匹配 daemon_id 改为返回所有 local_directory 资源（让 daemon 端自行按 daemon_id 过滤）。

具体变更：移除 daemon_id 匹配判断，直接保留所有 local_directory 资源。

**优点**：
- 改动小，仅去掉过滤逻辑
- 兼容现有存量数据
- 项目级 local_directory 允许多 daemon 共享是合理的

**缺点**：
- 如果项目中有多个 local_directory（每个 daemon 一个），各 daemon 会看到所有路径
  但 daemon 端的 findLocalDirectoryAssignment 会按 daemon_id 匹配，只取自己的

### 方案 B：local_directory 改为多 daemon 共享模型

在 project_resource 中为 local_directory 引入多 daemon 关联表，或标记为共享。

**优点**：语义清晰，权限精确
**缺点**：改动大，需新增迁移、查询、API

### 方案 C：Daemon 端降级处理

Daemon 端在 findLocalDirectoryAssignment 返回 nil 时，尝试从 project_resources 中找任意一个 local_directory（不按 daemon_id 过滤）。

**优点**：不修改后端过滤逻辑
**缺点**：daemon 端处理过于宽泛，可能拿到错误的目录

## 推荐方案 A 的详细变更

### server/internal/handler/daemon.go - filterProjectResourcesForRuntime

将函数中对 local_directory 的 daemon_id 过滤移除，改为对所有资源全部返回：

`go
func (h *Handler) filterProjectResourcesForRuntime(ctx context.Context, projectID pgtype.UUID, runtime db.AgentRuntime) []db.ProjectResource {
    rows := h.listProjectResourcesForProject(ctx, projectID)
    if len(rows) == 0 {
        return nil
    }
    // local_directory 是项目级别资源，允许多 daemon 共享。
    // 不再按 daemon_id 过滤，daemon 端会自行按 daemon_id 匹配。
    return rows
}
`

### daemon/local_directory.go - findLocalDirectoryAssignment

daemon 端已经按 daemon_id 过滤了，所以即使收到多个 local_directory 也会只取自己的。**无需修改**。

## 验收条件

1. 项目配置了 local_directory（属于用户 A 的 daemon D1）
2. 用户 B 触发分配，resolver 路由到 B 的运行时（daemon D2）
3. Task 创建成功，且 GetIssueDetail / GetProjectResources 返回的 resources 中包含该 local_directory
4. Daemon D2 claim task 后，findLocalDirectoryAssignment 能拿到该 local_directory
5. Agent 在正确的本地目录下执行代码编写任务
6. 原有场景（同 daemon 执行）不受影响
