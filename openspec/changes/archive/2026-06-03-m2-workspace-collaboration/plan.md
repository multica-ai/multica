# M2 Workspace Collaboration — Implementation Plan

## 改动概述

引入 Workspace 邀请链接机制，修复隐式成员添加问题，增加透明度字段。

核心变更：
1. DB Migration：给 `workspace` 加 `invite_token`，给 `member` 加 `invited_by`
2. 后端：新增 invite link 相关 handler，修改 `CreateMember` 移除 auto-create
3. 前端：members-tab 新增邀请链接区域，新增 `/invite/:token` 页面

---

## 文件改动清单

### 1. 数据库 Migration

**新增：** `server/migrations/037_member_invite.up.sql`

```sql
-- Add invite token to workspace
ALTER TABLE workspace ADD COLUMN invite_token TEXT UNIQUE;

-- Add invited_by tracking to member
ALTER TABLE member ADD COLUMN invited_by UUID REFERENCES "user"(id) ON DELETE SET NULL;
```

**新增：** `server/migrations/037_member_invite.down.sql`

```sql
ALTER TABLE member DROP COLUMN IF EXISTS invited_by;
ALTER TABLE workspace DROP COLUMN IF EXISTS invite_token;
```

### 2. SQL Queries

**修改：** `server/pkg/db/queries/workspace.sql`

新增以下查询：
```sql
-- name: GetWorkspaceByInviteToken :one
SELECT * FROM workspace WHERE invite_token = $1;

-- name: SetWorkspaceInviteToken :one
UPDATE workspace SET invite_token = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ClearWorkspaceInviteToken :one
UPDATE workspace SET invite_token = NULL, updated_at = now() WHERE id = $1 RETURNING *;
```

**修改：** `server/pkg/db/queries/member.sql`

修改 `CreateMember` 查询加入 `invited_by`：
```sql
-- name: CreateMemberWithInvitedBy :one
INSERT INTO member (workspace_id, user_id, role, invited_by)
VALUES ($1, $2, $3, $4)
RETURNING *;
```

注意：原 `CreateMember` query 保留不变（`invited_by` 列有默认 NULL）。

新增查询：
```sql
-- name: ListMembersWithUser :many  (更新现有查询，加入 invited_by)
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at, m.invited_by,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;
```

**之后必须运行：**
```bash
make sqlc
```

### 3. 后端 Handler

**修改：** `server/internal/handler/workspace.go`

**改动 1：`CreateMember` handler — 移除 auto-create 逻辑**

```go
// 旧代码（移除）：
user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{
    Name:  email,
    Email: email,
})

// 新代码：
if isNotFound(err) {
    writeError(w, http.StatusNotFound, "user not found: this email is not registered")
    return
}
```

同时，在 `CreateMember` 中将 `invited_by` 设为当前操作者的 user_id（使用新增的 `CreateMemberWithInvitedBy` query）。

**改动 2：新增 invite link 相关 handler**

新增函数：
- `GetInviteInfo(w, r)` — 公开端点，按 token 查询 workspace 基本信息
  - `GET /api/invite/:token`
  - 响应：`{ id, name, slug, member_count }`
- `JoinByInviteToken(w, r)` — 需登录，加入 workspace
  - `POST /api/invite/:token/join`
  - 响应：201 `MemberWithUserResponse`，或 200 若已是成员
- `ResetInviteLink(w, r)` — 需 admin/owner，重置 token（`gen_random_uuid()`）
  - `POST /api/workspaces/:id/invite-link/reset`
  - 响应：200 `{ invite_token, invite_link }`
- `DisableInviteLink(w, r)` — 需 admin/owner，停用邀请链接（设 NULL）
  - `DELETE /api/workspaces/:id/invite-link`
  - 响应：204

**改动 3：`MemberWithUserResponse` 新增 `InvitedBy` 字段**

```go
type MemberWithUserResponse struct {
    // ...现有字段
    InvitedBy *string `json:"invited_by"` // nullable user_id
}
```

**改动 4：`WorkspaceResponse` 新增 `InviteToken` 字段**

```go
type WorkspaceResponse struct {
    // ...现有字段
    InviteToken *string `json:"invite_token"` // nullable
}
```

### 4. 后端路由

**修改：** `server/cmd/server/router.go`

新增路由：
```go
// 公开路由（不需要认证）
r.Get("/api/invite/{token}", h.GetInviteInfo)

// 需要认证的路由（已登录用户）
r.Post("/api/invite/{token}/join", h.JoinByInviteToken)

// 工作区管理路由（需要 admin/owner）
r.Post("/api/workspaces/{id}/invite-link/reset", h.ResetInviteLink)
r.Delete("/api/workspaces/{id}/invite-link", h.DisableInviteLink)
```

注意：invite join 路由放在 protected 路由组中（需要 JWT）。
GetInviteInfo 放在公开路由组中。

### 5. 前端类型

**修改：** `apps/workspace/src/shared/types/workspace.ts`

```typescript
export interface Workspace {
  // ...现有字段
  invite_token: string | null; // nullable
}

export interface MemberWithUser {
  // ...现有字段
  invited_by: string | null; // nullable user_id
}
```

**修改：** `apps/workspace/src/shared/types/api.ts`

新增：
```typescript
export interface WorkspaceInviteInfo {
  id: string;
  name: string;
  slug: string;
  member_count: number;
}
```

### 6. 前端 API Client

**修改：** `apps/workspace/src/shared/api/client.ts`

新增方法：
```typescript
// 获取邀请链接对应的 workspace 信息（公开）
async getInviteInfo(token: string): Promise<WorkspaceInviteInfo>

// 通过邀请链接加入 workspace（需登录）
async joinByInviteToken(token: string): Promise<MemberWithUser>

// 重置邀请链接（需 admin）
async resetInviteLink(workspaceId: string): Promise<{ invite_token: string }>

// 停用邀请链接（需 admin）
async disableInviteLink(workspaceId: string): Promise<void>
```

### 7. 前端 Settings Mutations

**修改：** `apps/workspace/src/features/settings/mutations.ts`

在 `useWorkspaceSettingsMutations` 中新增：
- `resetInviteLink()` — 调用 API，成功后更新 workspace store 中的 invite_token
- `disableInviteLink()` — 调用 API，成功后将 invite_token 设为 null

### 8. 前端 Members Tab UI

**修改：** `apps/workspace/src/features/settings/components/members-tab.tsx`

**新增：InviteLink 组件**（内联在文件中，约 60-80 行）

```tsx
function InviteLinkSection({ workspaceId, inviteToken, canManage }: ...) {
  // 展示邀请链接，复制按钮，重置/停用按钮
  // 无 token 时：显示 "Invite link disabled" + "Generate" 按钮
}
```

在 `MembersTab` 的 `<section>` 之前插入此区域。

**修改："Add member" 卡片**
- 错误提示文字：当 API 返回 404 时显示 "This email is not registered. Share the invite link instead."

### 9. 前端邀请页面

**新建目录：** `apps/workspace/src/features/invite/`

**新建文件：** `apps/workspace/src/features/invite/components/invite-page.tsx`

```tsx
// InvitePage：
// 1. 根据 URL 中的 token 查询 workspace 信息（useQuery）
// 2. 未登录：redirect 到 /login?redirect=/invite/:token
// 3. 已登录 + workspace 已加入：直接跳转到 /
// 4. 已登录 + workspace 未加入：显示 workspace 名称 + "Join" 按钮
// 5. token 无效：显示错误
```

**修改：** `apps/workspace/src/router.tsx`

新增路由：
```typescript
const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "invite/$token",
  component: InvitePage,
});
```

注意：invite 路由挂在 `rootRoute`（不需要认证，由页面内部处理重定向）。

---

## 执行顺序

1. **Migration** → `037_member_invite.up.sql` / `.down.sql`
2. **SQL Queries** → 修改 workspace.sql + member.sql（ListMembersWithUser）
3. **`make sqlc`** → 重新生成 generated/ 代码
4. **Go Handler** → 修改 workspace.go（CreateMember + 新 invite handler 函数）
5. **Go Router** → 修改 router.go 注册路由
6. **Go Tests** → `cd server && go test ./internal/handler/ -run TestMember`（如有）
7. **TS Types** → 修改 workspace.ts + api.ts
8. **TS API Client** → 修改 client.ts
9. **TS Mutations** → 修改 mutations.ts
10. **TS Members Tab** → 修改 members-tab.tsx
11. **TS Invite Page** → 新建 invite/components/invite-page.tsx
12. **TS Router** → 修改 router.tsx 注册路由
13. **验证** → `pnpm typecheck` + `make test`

---

## 风险点

| 风险 | 影响 | 对策 |
|---|---|---|
| `make sqlc` 再生成影响 `ListMembersWithUser` 的所有调用方 | 中 | 更新 query 后检查所有使用 `ListMembersWithUser` 的 handler 是否需要更新 `invited_by` |
| 移除 auto-create 可能破坏现有 E2E 测试或测试夹具 | 中 | grep 全部 `createMember` 调用，确认测试中是否依赖 auto-create 行为 |
| 邀请页面在未登录状态下的 redirect 循环 | 低 | 登录后的 redirect 逻辑需要保留 `?redirect=` 参数并处理 |
| WorkspaceResponse 新增 `invite_token` 字段会暴露 token 给所有成员 | 中 | GetInviteInfo 只返回 workspace 基本信息；invite_token 字段仅在 admin/owner 可见的 API 响应中携带（或前端仅在 settings 页面展示） |

最后一条风险是关键的安全决策：**invite_token 不能对 member 角色可见**。解决方案：
- workspace 基本信息 API 返回的 invite_token 字段按角色过滤（handler 中判断）
- 或者新增独立的 `GET /api/workspaces/:id/invite-link` 端点（仅 admin/owner 调用）

推荐方案：新增独立端点，而非在 `WorkspaceResponse` 中携带 token。

---

STATUS: WAITING FOR USER APPROVAL
