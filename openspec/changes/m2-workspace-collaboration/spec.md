# M2 Workspace Collaboration — Product Spec

## 背景与问题

当前 Workspace 成员管理完全是隐式的：
- 管理员在 Settings > Members 输入邮箱，后端直接创建 `member` 记录
- 如果邮箱对应用户不存在，后端**自动 CreateUser**（name 字段填邮箱字符串）
- 被"邀请"的人没有任何通知，不知道自己已经被加入
- 成员记录没有 `status` 或 `invited_by` 字段，无法区分"谁邀请的谁"
- 生命周期完全隐式：加入即 active，没有主动接受步骤

Exit criteria：
- 成员生命周期不再隐式发生
- 邀请和访问状态对管理员与成员都清晰可见
- 角色边界明确、行为可预测
- Workspace 设置能清楚区分团队级配置与个人级配置

---

## 现状快照（探索发现）

| 层 | 现状 |
|---|---|
| `member` 表字段 | `id, workspace_id, user_id, role, created_at` |
| 角色 | `owner \| admin \| member` （check constraint 已存在） |
| member status | **不存在** |
| invited_by | **不存在** |
| 邀请 token/链接 | **不存在** |
| Auto-create user | 是 —— `CreateMember` handler 在邮箱用户不存在时调用 `CreateUser` |
| 成员 UI | 展示列表 + 改角色 + 移除，有角色检查；没有 pending 状态显示 |
| 已有 owner 保护 | 是 —— countOwners 防止 workspace 失去 owner |

---

## 决策

### 1. 邀请模型：Workspace-level 邀请链接

**选择：单一 workspace 邀请链接（invite link），不做带 pending 状态的邮件邀请。**

理由：
- 不依赖邮件服务（项目无 SMTP 基础设施）
- 最小实现：1 个 token 字段 + 2 个 API + 1 个前端页面
- 适合 2-10 人小团队面对面/IM 分享链接

具体模型：
- `workspace.invite_token TEXT UNIQUE`（nullable，NULL 表示邀请链接已停用）
- Token：随机 UUID（`gen_random_uuid()`），短 UUID 足够，不过期（管理员手动重置）
- 邀请链接格式：`{frontend_url}/invite/{token}`
- 任何已登录用户访问链接后调用 join API → 以 `member` 角色加入 workspace
- 如果用户已是成员：join API 静默成功（幂等），重定向到 workspace

同时修复：移除 `CreateMember` 中的 auto-create 逻辑，若邮箱用户不存在返回 404 错误。管理员只能按邮箱添加**已注册用户**。这样"按邮箱添加"也变得显式（需要对方已注册）。

### 2. 成员状态机

**不引入 `pending` 状态。** 邀请链接模式下，点击链接 = 主动加入 = 立即 active。无需 pending。

简单状态机：`(不存在) → active（加入后）→ (被移除)`

### 3. 角色设计（三级，现有实现保持不变）

| 角色 | 能做什么 |
|---|---|
| **owner** | 全权：管理所有成员（含 owner）、删除 workspace、改所有设置 |
| **admin** | 管理 member/admin 成员、改 workspace 设置；不能操作 owner 级别 |
| **member** | 创建/处理 issues；不能管理成员 |

约束：workspace 必须至少有 1 个 owner（现有实现已强制）。

### 4. Owner 转移

**本里程碑不实现。** 超出当前范围，涉及额外安全确认流程。

### 5. 成员 invited_by 记录

给 `member` 表加 `invited_by UUID REFERENCES "user"(id)`（nullable）：
- 通过 join API 加入：`invited_by = NULL`（自主加入）
- 通过 CreateMember（管理员按邮箱添加）加入：`invited_by = 操作者 user_id`

这增加了透明度，记录成员来源，但不影响访问控制逻辑。

---

## UI 设计

### Settings > Members Tab

**区域 1（新增）：Invite Link**
- 显示：邀请链接文字 + 复制按钮
- 如果链接未生成（invite_token = NULL）：显示"邀请链接未启用"+ "Generate link" 按钮
- 如果链接已生成：显示完整链接（截断显示）+ 复制按钮 + "Reset" 按钮 + "Disable" 按钮
- 仅 admin/owner 可见此区域

**区域 2（已有，修改说明文字）：Add registered member**
- 说明文字改为"Enter the email of a registered user"
- 如果邮箱用户不存在，显示错误"This email is not registered. Share the invite link instead."

**区域 3（已有）：成员列表**
- 新增每个成员的 `invited_by` 信息（可选：小字展示"Added by X"，空间允许时显示）

### 新增页面：`/invite/:token`

- 展示 workspace 名称
- 已登录：显示"Join [Workspace Name]"按钮
- 未登录：重定向到 `/login?redirect=/invite/:token`，登录后再回来
- 加入后跳转到 workspace 主页（`/`）
- Token 无效或 workspace 邀请链接已停用：显示错误页

---

## 不做的事（范围外）

- 邮件通知（无 SMTP 基础设施）
- 邀请带过期时间（V1 不需要，依赖管理员手动重置）
- 带 pending 状态的邮件邀请流程
- Owner 转移
- 邀请特定邮箱并预设角色的邀请流程（V1 加入统一为 member 角色）
- 单点登录 / SAML
