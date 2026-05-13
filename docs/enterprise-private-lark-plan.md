# Multica 私有化与飞书集成从 0 到 1 技术方案

## 1. 背景

Multica 当前是 Go 后端 + pnpm monorepo 前端架构，核心对象包括用户、工作区、成员、issue、agent、runtime、inbox。私有化改造目标是在不破坏开源主干同步能力的前提下，接入企业飞书账号体系、飞书消息通知、外部系统同步创建 issue/任务，以及企业内部 agent runtime 管理。

当前业务阶段分两步：

1. 前期：每个员工本地 agent runtime 注册到内网部署的 Multica，由团队在 Multica 中指派开发任务。
2. 后期：内部私有公共云端 agent runtime 注册到 Multica，团队成员可能共用同一个或一组公共 agent 进行开发。

## 2. 建设目标

### 2.1 功能目标

- 支持飞书 OAuth 登录，并将飞书用户身份与 Multica 用户绑定。
- 支持飞书组织成员同步，降低手动邀请和账号维护成本。
- 支持通过邀请邮箱定位对应飞书用户，并发送飞书邀请消息。
- 支持 Multica 通知追加飞书消息投递。
- 支持外部系统通过接口幂等创建或同步 Multica issue。
- 支持外部系统创建 issue 后自动指派 agent 并进入现有 agent task queue。
- 支持本地 runtime 和企业公共 cloud runtime 两种阶段性部署模式。
- 支持企业级审计、token 管理、权限控制和可回滚配置。

### 2.2 工程目标

- 保持对开源版本的长期同步能力。
- 核心业务模型少改，优先新增扩展点和独立表。
- 私有化能力通过 feature flag 控制，默认关闭。
- 飞书、企业 runtime、外部接口等私有逻辑尽量集中在独立目录。
- 所有外部输入具备幂等、鉴权、审计和错误重试能力。

## 3. 非目标

- 不重写 Multica 的用户、workspace、issue、agent 核心模型。
- 不把 Multica 内部 user.id 替换为飞书 open_id 或 union_id。
- 不在业务 handler 中散落飞书 API 调用。
- 不让公共 agent 共享同一个不隔离的执行上下文。
- 不为了私有化能力长期维护一套无法同步开源主干的深度 fork。

## 4. 当前代码基线

### 4.1 账号体系

当前登录入口集中在 `server/internal/handler/auth.go`：

- 邮箱验证码：`SendCode` / `VerifyCode`
- Google OAuth：`GoogleLogin`
- 用户创建：`findOrCreateUser(email)`
- 登录态：后端签发 Multica 自有 JWT

改造策略：保留 Multica JWT 和内部 user.id，只新增外部身份绑定表与飞书 OAuth provider。

### 4.2 通知体系

当前通知由 `server/cmd/server/notification_listeners.go` 监听业务事件后创建 `inbox_item`，并通过 WS 推送 `inbox:new`。通知偏好在 `notification_preference` 表中维护。

改造策略：把飞书消息作为 inbox 创建后的第二投递通道，通过 outbox/worker 异步发送。

### 4.3 Issue 与任务创建

当前用户态 issue 创建入口是 `POST /api/issues`，handler 为 `CreateIssue`。创建 issue 后，如果 assignee 是 agent，会调用 `TaskService.EnqueueTaskForIssue` 进入 agent task queue。

改造策略：新增外部系统专用 ingress API，复用现有 issue 创建和 task enqueue 逻辑，但加入 integration token、外部 ID 幂等和审计。

### 4.4 Runtime 与 Agent

当前 runtime 注册由 daemon 调用 `/api/daemon/register`，`agent_runtime` 已有 `workspace_id`、`daemon_id`、`runtime_mode`、`provider`、`owner_id` 等字段。agent 创建时绑定 `runtime_id`。

改造策略：前期沿用本地 runtime owner 模型；后期扩展 runtime pool、公共 runtime 策略和 task 级隔离。

## 5. 总体架构

```text
                 +----------------------+
                 |       飞书租户        |
                 | OAuth / Bot / Event  |
                 +----------+-----------+
                            |
                            v
+---------------------------+---------------------------+
|                 Multica 私有化部署                     |
|                                                       |
|  Auth Provider        Notification Dispatcher          |
|  - lark oauth         - inbox                          |
|  - identity binding   - lark channel                   |
|  - email -> lark user - invite delivery                |
|                                                       |
|  Integration Ingress   Core Domain                     |
|  - integration token   - workspace/member/user         |
|  - issue sync API      - issue/agent/runtime/task      |
|  - external link       - inbox/activity                |
|                                                       |
|  Runtime Layer                                         |
|  - local daemon runtime                                |
|  - private cloud runtime pool                          |
+---------------------------+---------------------------+
                            |
                            v
                +-----------+------------+
                | 本地 daemon / 云端 runtime |
                +------------------------+
```

## 6. Fork 与上游同步策略

### 6.1 仓库策略

必须 fork 仓库，但不要做深度魔改 fork。

推荐 remote/branch：

```text
upstream/main              # kanfashidoufu/multica 开源主干
company/main               # 企业私有 fork 主干，定期同步 upstream
company/enterprise/lark    # 飞书与企业私有化开发分支
company/release/internal   # 内网发布分支
```

### 6.2 代码边界

优先把通用扩展点做成可上游化能力：

```text
server/internal/identity        # 外部身份 provider 接口
server/internal/notifications   # 通知 dispatcher/channel/outbox
server/internal/integrations    # 外部 ingress/token/scope
server/internal/runtimepolicy   # runtime 策略
```

私有实现集中放置：

```text
server/internal/enterprise/lark
server/internal/enterprise/runtime
```

如果后续开源主干不接受 `enterprise` 目录，可以在私有 fork 中保留；但接口层尽量保持通用、可上游。

### 6.3 同步流程

每周或每两周同步一次上游：

```bash
git fetch upstream
git checkout company/main
git merge upstream/main
make check
```

控制原则：

- 避免频繁改 `issue.go`、`auth.go`、`agent.go`、`router.go` 的核心流程。
- 需要挂载私有能力时，优先通过接口注册和 feature flag。
- 数据库新增表优先，少改现有核心字段语义。
- 私有 patch 维持小而集中，降低冲突概率。

## 7. 数据模型设计

### 7.1 外部身份绑定

```sql
CREATE TABLE user_external_identity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    tenant_key TEXT NOT NULL,
    external_user_id TEXT,
    open_id TEXT,
    union_id TEXT,
    email TEXT,
    name TEXT,
    avatar_url TEXT,
    raw_profile JSONB NOT NULL DEFAULT '{}',
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider, tenant_key, union_id),
    UNIQUE(provider, tenant_key, open_id)
);
```

说明：

- `user_id` 仍然是 Multica 内部主键。
- `union_id` 优先用于同一企业下稳定识别。
- `open_id` 用于飞书消息发送。
- `email` 只用于首次匹配，不作为长期唯一外部身份。

### 7.2 Workspace 飞书集成配置

```sql
CREATE TABLE workspace_lark_integration (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tenant_key TEXT NOT NULL,
    app_id TEXT NOT NULL,
    credential_ref TEXT,
    bot_open_id TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    auto_join_enabled BOOLEAN NOT NULL DEFAULT false,
    default_role TEXT NOT NULL DEFAULT 'member',
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, tenant_key)
);
```

说明：

- 私有化单租户部署可先只支持一个 `tenant_key`。
- `credential_ref` 指向 KMS/Secret Manager，不建议明文保存 app_secret。
- `auto_join_enabled` 控制飞书成员首次登录是否自动加入 workspace。

### 7.3 通知投递 Outbox

```sql
CREATE TABLE notification_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    inbox_item_id UUID NOT NULL REFERENCES inbox_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    recipient_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    dedupe_key TEXT NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(channel, dedupe_key)
);
```

说明：

- `channel` 可为 `lark`、`email`、`webhook`。
- `dedupe_key` 可用 `lark:<inbox_item_id>:<recipient_user_id>`。
- worker 异步消费，业务请求不等待飞书 API。

### 7.4 外部系统集成 Token

```sql
CREATE TABLE integration_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL,
    created_by UUID REFERENCES "user"(id),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

推荐 token 前缀：

```text
mli_  # Multica Local Integration token
```

Scope 示例：

```text
issues:write
issues:read
comments:write
attachments:write
```

### 7.5 外部 Issue 关联表

```sql
CREATE TABLE external_issue_link (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    external_type TEXT NOT NULL,
    external_id TEXT NOT NULL,
    external_url TEXT,
    payload_hash TEXT,
    raw_payload JSONB NOT NULL DEFAULT '{}',
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, provider, external_type, external_id)
);
```

说明：

- 外部系统重复推送时通过唯一键幂等。
- 不建议继续扩展 `issue.origin_id`，因为当前是 UUID，且已被 quick-create/autopilot 使用。

### 7.6 Runtime 策略

```sql
CREATE TABLE workspace_runtime_policy (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    local_runtime_enabled BOOLEAN NOT NULL DEFAULT true,
    cloud_runtime_enabled BOOLEAN NOT NULL DEFAULT false,
    default_agent_id UUID REFERENCES agent(id),
    max_concurrent_tasks_per_runtime INT NOT NULL DEFAULT 20,
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id)
);
```

后期如需 runtime pool，可继续新增：

```text
runtime_pool
runtime_pool_member
agent_runtime_pool_binding
```

### 7.7 飞书邀请投递记录

工作区邀请当前基于 `workspace_invitation`，被邀请人可能还没有 Multica 账号，也可能还不是 workspace member，因此不要复用必须绑定 `inbox_item` 的 `notification_delivery`。建议为邀请消息单独建投递表：

```sql
CREATE TABLE lark_invitation_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invitation_id UUID NOT NULL REFERENCES workspace_invitation(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tenant_key TEXT NOT NULL,
    invitee_email TEXT NOT NULL,
    lark_open_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    dedupe_key TEXT NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    sent_message_id TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(dedupe_key)
);
```

说明：

- `dedupe_key` 使用 `lark-invite:<invitation_id>`，避免重复点击邀请按钮或 worker 重试导致重复私聊。
- `lark_open_id` 可在发送前通过邮箱解析得到；如果被邀请人尚未创建 Multica user，不要强行写 `user_external_identity`。
- 邀请被接受、拒绝、撤销或过期后，pending delivery 应停止发送。
- 发送失败不影响 `workspace_invitation`，仍可通过邮件或手动复制邀请链接兜底。

## 8. 后端接口设计

### 8.1 飞书登录

新增接口：

```text
POST /auth/lark
```

请求：

```json
{
  "code": "oauth-code",
  "redirect_uri": "https://multica.internal/auth/lark/callback"
}
```

响应沿用现有 `LoginResponse`：

```json
{
  "token": "...",
  "user": {}
}
```

处理流程：

1. 校验 `LARK_AUTH_ENABLED`。
2. 用 code 换飞书 access token。
3. 获取飞书用户信息。
4. 校验 tenant 是否在 allowlist。
5. 通过 `user_external_identity` 查绑定。
6. 未绑定时按 email 匹配已有 Multica 用户。
7. 仍不存在时按 signup 策略创建用户。
8. upsert `user_external_identity`。
9. 如 workspace 开启 auto join，则创建 member。
10. 签发 Multica JWT 与 cookie。

### 8.2 集成 Token 管理

新增 workspace admin 接口：

```text
GET    /api/workspaces/{id}/integration-tokens
POST   /api/workspaces/{id}/integration-tokens
DELETE /api/workspaces/{id}/integration-tokens/{tokenId}
```

创建响应只返回一次明文 token，后续只保存 hash。

### 8.3 外部 Issue 同步创建

新增集成接口：

```text
POST /api/integrations/issues/sync
Authorization: Bearer mli_xxx
Idempotency-Key: lark-task-xxx
```

请求：

```json
{
  "external_source": "lark",
  "external_type": "approval",
  "external_id": "approval-instance-id",
  "external_url": "https://example.feishu.cn/approval/xxx",
  "title": "开发客户合同审批自动归档能力",
  "description": "来自飞书审批，通过后自动创建开发任务。",
  "priority": "medium",
  "status": "todo",
  "project_id": null,
  "parent_external_id": null,
  "due_date": "2026-05-15T18:00:00+08:00",
  "assignee": {
    "type": "agent",
    "id": "agent-uuid"
  },
  "creator": {
    "provider": "lark",
    "open_id": "ou_xxx"
  },
  "labels": ["飞书同步", "审批"],
  "raw_payload": {}
}
```

响应：

```json
{
  "action": "created",
  "issue": {
    "id": "...",
    "identifier": "MUL-123",
    "title": "..."
  },
  "task_id": "..."
}
```

幂等规则：

- 以 `workspace_id + external_source + external_type + external_id` 为主幂等键。
- 如果 link 不存在：创建 issue + link。
- 如果 link 存在且 payload hash 未变化：返回 `action=noop`。
- 如果 link 存在且 payload hash 变化：按字段策略更新 issue，返回 `action=updated`。
- 如果 issue 已完成，默认不允许外部系统重开，除非请求显式 `reopen=true` 且 token 有 `issues:reopen` scope。

字段更新策略：

| 字段 | 默认策略 |
| --- | --- |
| title | 外部系统可覆盖 |
| description | 外部系统可覆盖或追加同步区块 |
| priority | 外部系统可覆盖 |
| status | 只允许 backlog/todo/in_progress 之类安全前进，禁止覆盖 done/cancelled |
| assignee | 首次创建设置，后续默认不覆盖 |
| labels | 追加，不删除人工标签 |
| due_date | 外部系统可覆盖 |

### 8.4 外部系统回写 Webhook

可选新增：

```text
POST /api/integrations/webhooks/lark
```

用于接收飞书事件，例如审批通过、多维表格行变更、飞书任务状态变更。

为了降低初期复杂度，建议 Phase 1 不直接接收飞书 webhook，而是由内部集成服务调用 `/api/integrations/issues/sync`。稳定后再把飞书 webhook 直接接入 Multica。

### 8.5 通过飞书邮箱发送邀请消息

目标：管理员在 Multica 输入邮箱邀请成员时，如果该邮箱属于当前飞书租户内的用户，系统除发送原有邮件外，再通过飞书机器人给对应飞书用户发送一条邀请卡片。

触发点：

1. `CreateInvitation` 先按现有逻辑校验权限、创建 `workspace_invitation`、返回 201。
2. 如果 `LARK_INVITE_NOTIFY_ENABLED=true` 且 workspace 已启用飞书集成，写入 `lark_invitation_delivery`。
3. 后台 worker 异步解析收件人并发送飞书消息；业务请求不等待飞书 API。

收件人解析：

1. 对 `invitee_email` 做 lowercase + trim，且必须匹配企业允许的邮箱域。
2. 优先查已有 `user_external_identity`，如果该邮箱已绑定当前 `tenant_key` 且有 `open_id`，直接使用。
3. 未命中时调用飞书通讯录接口“通过手机号或邮箱获取用户 ID”，用 `emails=[invitee_email]` 和 `user_id_type=open_id` 获取 `open_id`。
4. 如果未找到用户、用户已离职、或邮箱属于其他租户，delivery 标记为 `skipped` 或 `failed`，保留普通邮件邀请兜底。
5. 如果找到用户但 Multica user 尚不存在，只把 `open_id` 写入 `lark_invitation_delivery`；等对方通过飞书登录或邮箱登录后，再建立正式 `user_external_identity` 绑定。

发送方式：

```text
POST /open-apis/im/v1/messages?receive_id_type=open_id
Authorization: Bearer <tenant_access_token>
```

请求体建议使用卡片消息：

```json
{
  "receive_id": "ou_xxx",
  "msg_type": "interactive",
  "content": "{\"config\":{\"wide_screen_mode\":true},\"elements\":[...],\"header\":{\"title\":{\"tag\":\"plain_text\",\"content\":\"你被邀请加入 Multica 工作区\"}}}"
}
```

卡片内容：

- 标题：你被邀请加入 Multica 工作区。
- 正文：`{inviter_name}` 邀请你以 `{role}` 身份加入 `{workspace_name}`。
- 过期提示：邀请 7 天内有效。
- 按钮：打开 `${FRONTEND_ORIGIN}/invite/{invitation_id}`。

未注册用户点击链路：

1. 用户从飞书卡片点击 `${FRONTEND_ORIGIN}/invite/{invitation_id}`。
2. 如果当前没有 Multica 登录态，前端跳转到 `${FRONTEND_ORIGIN}/login?next=/invite/{invitation_id}`。
3. 私有化部署如果只允许飞书登录，可在登录页直接展示或自动触发飞书 OAuth。
4. 飞书 OAuth 回调后，后端按飞书邮箱执行 `findOrCreateUser(email)`，创建 Multica user，并 upsert `user_external_identity`。
5. 前端回到 `next=/invite/{invitation_id}`，邀请页校验 `invitee_email == current_user.email` 或 `invitee_user_id == current_user.id`。
6. 用户点击接受后，后端创建 workspace member，并把邀请状态改为 `accepted`。

因此，被邀请人不需要先手动注册 Multica；只要该邮箱能通过飞书 OAuth 证明身份，首次点击邀请链接即可完成“飞书登录 -> Multica 账号创建 -> 接受邀请”的闭环。

实现约束：

- 飞书应用需要安装到目标租户，并具备“按邮箱获取用户 ID”和“发送消息”的相关权限。
- 推荐先解析成 `open_id` 再发送，便于审计和错误归因；如果企业确认飞书消息接口支持 `receive_id_type=email`，可作为简化路径，但仍要记录最终解析/发送结果。
- 使用飞书消息接口的 `uuid` 或本地 `dedupe_key` 做幂等，避免重试重复推送。
- 邀请卡片只包含 workspace、邀请人、角色、过期时间和内网链接，不放 issue 内容、agent 输出或敏感配置。
- 飞书发送失败只影响该 delivery，不回滚邀请创建；管理员界面应显示“邮件已发，飞书消息失败/未找到飞书用户”的可观测状态。

## 9. 通知设计

### 9.1 通知分发接口

新增通用接口：

```go
type NotificationMessage struct {
    InboxItemID string
    WorkspaceID string
    RecipientUserID string
    Type string
    Severity string
    Title string
    Body string
    IssueID string
    IssueIdentifier string
    ActorType string
    ActorID string
    URL string
}

type NotificationChannel interface {
    Name() string
    Send(ctx context.Context, msg NotificationMessage) error
}
```

飞书实现：

```text
server/internal/enterprise/lark/notification_channel.go
```

### 9.2 触发范围

首批建议只推送高价值通知：

- issue assigned
- mentioned
- new comment
- task failed
- agent completed / quick-create completed

后续再开放：

- status changed
- priority changed
- due date changed
- reaction added

### 9.3 飞书消息格式

推荐卡片消息：

```text
标题：你被指派了 MUL-123
内容：开发客户合同审批自动归档能力
触发人：张三
状态：Todo
按钮：打开 Multica
```

安全策略：

- 不发送完整长评论。
- 不发送 agent 输出全文。
- 不发送 secret/env/mcp_config。
- 描述过长时截断到 200 字。
- 链接使用内网 Multica URL。

### 9.4 偏好设置

现有偏好是事件组开关。建议兼容旧格式，新增 channel 维度：

```json
{
  "assignments": {
    "in_app": "all",
    "lark": "all"
  },
  "comments": {
    "in_app": "all",
    "lark": "muted"
  }
}
```

兼容策略：

- 旧值 `"muted"` 等价于所有 channel muted。
- 旧值缺失等价于所有 channel all。
- 后端读取时做 normalize，前端逐步迁移 UI。

## 10. Runtime 私有化设计

### 10.1 阶段一：员工本地 Runtime

目标：每个员工本地 daemon 注册到内网 Multica。

实施策略：

- 飞书登录后，用户在 Multica 中创建/加入 workspace。
- 用户本地 CLI 或 Desktop 登录 Multica。
- daemon 以用户 PAT/JWT 或 daemon token 注册 runtime。
- `agent_runtime.owner_id` 记录 runtime 所属员工。
- agent 默认 private，owner/admin 可管理。
- 任务指派给员工创建的 agent，即派发到员工本地 runtime。

需要补齐：

- 企业禁止普通邮箱验证码注册，只允许飞书 tenant。
- 管理员可查看 runtime owner、设备、provider、版本、在线状态。
- 支持吊销某个 daemon token。
- 支持离职用户 runtime 自动停用或隐藏。

### 10.2 阶段二：内部公共 Cloud Runtime

目标：团队成员通过同一个或一组公共 agent 执行任务。

关键原则：

- 共享 agent profile，不共享 task execution context。
- 每个 task 必须有独立 worktree/container/sandbox。
- 每个 task 记录 requester、creator、agent、runtime、cost。
- agent session 默认按 issue/task 隔离，避免跨用户上下文污染。
- secret 通过 workspace/runtime secret manager 注入，不放普通 agent config。

建议模型：

```text
Runtime Pool: 公司内部云执行资源
Agent Profile: 共享指令、模型、工具、技能配置
Task Execution: 每次任务的隔离执行实例
```

新增能力：

- workspace 默认 agent 配置。
- runtime pool 注册与健康检查。
- task 级资源限制。
- requester_id 审计。
- 成本归属统计。

## 11. Feature Flag 与配置

新增环境变量：

```text
ENTERPRISE_ENABLED=false
LARK_AUTH_ENABLED=false
LARK_NOTIFY_ENABLED=false
LARK_INVITE_NOTIFY_ENABLED=false
LARK_ORG_SYNC_ENABLED=false
INTEGRATION_ISSUE_INGRESS_ENABLED=false
ENTERPRISE_RUNTIME_POLICY_ENABLED=false

LARK_APP_ID=
LARK_APP_SECRET_REF=
LARK_TENANT_ALLOWLIST=
FRONTEND_ORIGIN=https://multica.internal
```

原则：

- 开源默认关闭。
- 私有部署按模块开启。
- 功能关闭时，路由返回 404 或 503，不影响开源路径。

## 12. 从 0 到 1 实施步骤

### Phase 0：Fork 与基础设施准备

目标：建立可持续同步上游的私有开发流。

任务：

1. Fork `kanfashidoufu/multica` 到企业 Git 服务。
2. 添加 upstream remote。
3. 建立 `company/main`、`company/enterprise/lark`、`company/release/internal` 分支。
4. 配置 CI，至少运行：
   - `pnpm typecheck`
   - `pnpm test`
   - `make test`
   - `make check`
5. 建立私有部署环境：
   - PostgreSQL
   - Redis
   - Multica server
   - Web app
   - 内网域名与 TLS
6. 输出第一版 `.env.private.example`。

验收：

- 内网可登录当前 Multica。
- 可创建 workspace、agent、issue。
- 本地 daemon 可注册 runtime。
- 能从 upstream/main 合并一次并通过 CI。

### Phase 1：企业登录与飞书身份绑定

目标：支持飞书登录，建立 Multica 用户与飞书用户的稳定映射。

任务：

1. 新增 migration：
   - `user_external_identity`
   - `workspace_lark_integration`
2. 新增 Go 接口：
   - `IdentityProvider`
   - `ExternalIdentity`
3. 实现飞书 OAuth provider。
4. 新增 `POST /auth/lark`。
5. 扩展 `GET /api/config` 返回：
   - `lark_auth_enabled`
   - `lark_app_id`
6. 前端 login page 增加飞书登录按钮。
7. 增加 tenant allowlist 校验。
8. 增加 auto join 策略，先支持配置级默认 workspace。
9. 保留管理员应急登录方式。

验收：

- 飞书用户可登录 Multica。
- 首次登录可自动创建或绑定 Multica user。
- 重复登录不会创建重复用户。
- 非 allowlist tenant 登录被拒绝。
- 关闭 `LARK_AUTH_ENABLED` 后开源登录路径不受影响。

### Phase 2：飞书通知与邀请投递

目标：Multica inbox 通知和工作区邀请可异步追加飞书消息。

任务：

1. 新增 migration：
   - `notification_delivery`
   - `lark_invitation_delivery`
2. 新增 notification dispatcher。
3. 新增 `NotificationChannel` 接口。
4. 实现 `LarkNotificationChannel`。
5. 在 inbox item 创建成功后写 delivery outbox。
6. 新增飞书邀请投递：
   - 邀请创建成功后写 `lark_invitation_delivery`
   - 通过邮箱解析飞书 `open_id`
   - 给对应飞书用户发送邀请卡片
   - 未找到飞书用户时保留邮件邀请兜底
7. 新增 worker：
   - 扫描 pending delivery
   - 调用飞书 bot API
   - 成功标记 sent
   - 失败记录 last_error 并指数退避
8. 扩展 notification preference 支持 channel 维度。
9. 前端 settings 通知页增加飞书渠道开关。
10. 加入消息模板与链接生成。

验收：

- 被指派 issue 时收到 Multica inbox 和飞书消息。
- @mention 时收到飞书消息。
- 邀请企业邮箱成员时，对应飞书用户收到邀请卡片。
- 飞书 API 失败不影响 issue 创建。
- 飞书邀请消息失败不影响邀请记录创建和邮件发送。
- delivery 重试可观测。
- 用户关闭飞书通知后不再发送。

### Phase 3：外部接口同步创建 Issue/任务

目标：内部系统可通过 API 幂等创建或同步 Multica issue，并可自动派发 agent 任务。

任务：

1. 新增 migration：
   - `integration_token`
   - `external_issue_link`
2. 新增 integration token 管理 API。
3. 新增 integration auth middleware：
   - 识别 `mli_` token
   - 校验 workspace
   - 校验 scopes
4. 新增 `POST /api/integrations/issues/sync`。
5. 抽取 issue 创建 service，复用 `CreateIssue` 现有校验：
   - assignee 校验
   - parent issue 校验
   - project 继承
   - issue counter
   - event publish
   - agent task enqueue
6. 实现幂等 upsert：
   - external link 不存在则创建
   - external link 存在则更新
   - payload hash 相同则 noop
7. 增加 label 追加策略。
8. 增加外部 creator 映射：
   - 根据飞书 open_id 找 `user_external_identity`
   - 找不到时使用 integration service account
9. 增加审计日志。

验收：

- 相同 external_id 重复请求只创建一个 issue。
- 首次同步创建 issue 后，如果 assignee 是 agent，会自动进入 task queue。
- 更新外部数据可同步修改 title/description/priority/due_date。
- 已完成 issue 默认不会被外部系统重开。
- 无 scope token 创建 issue 被拒绝。

### Phase 4：飞书组织同步

目标：降低手动成员维护成本。

任务：

1. 新增组织同步任务：
   - 拉取飞书部门
   - 拉取飞书用户
   - upsert external identity
2. 支持 workspace 成员同步规则：
   - 指定部门自动加入 workspace
   - 默认角色 member
   - 管理员白名单
3. 支持离职/停用处理：
   - 禁止登录
   - 可选移出 workspace
   - 停用其本地 runtime
4. 增加同步日志：
   - 成功数量
   - 失败数量
   - last_sync_at
5. 前端增加集成设置页。

验收：

- 指定飞书部门成员能自动进入 workspace。
- 离职用户不能继续登录。
- 同步失败有日志可查。

### Phase 5：本地 Runtime 企业治理

目标：在每个员工本地 runtime 阶段完成企业管控。

任务：

1. 新增 runtime policy 表。
2. 增加管理员 runtime 列表增强字段：
   - owner
   - device
   - provider
   - cli version
   - last seen
3. 增加 daemon token rotate/revoke API。
4. 增加离职用户 runtime 自动 offline 策略。
5. 增加 agent 创建策略：
   - 是否允许 member 创建 private agent
   - 是否必须绑定本人 runtime
   - 是否允许绑定公共 runtime
6. 增加审计：
   - runtime registered
   - runtime revoked
   - agent runtime changed

验收：

- 管理员可看清每个 runtime 属于谁。
- 被吊销 token 的 daemon 无法继续注册或 claim task。
- 普通成员不能绕过策略创建不合规 agent。

### Phase 6：内部公共 Cloud Runtime

目标：支持公司内部云端 agent runtime，团队共享 agent profile，但执行隔离。

任务：

1. 新增 cloud runtime 注册方式。
2. 增加 runtime pool 概念。
3. agent 可绑定 runtime pool 或指定 cloud runtime。
4. task claim 时按 pool 策略选择 runtime。
5. 每个 task 创建独立执行上下文：
   - worktree
   - container/sandbox
   - temp credentials
   - resource limits
6. 增加 requester_id 到 task 审计链路。
7. 增加使用量统计：
   - by requester
   - by agent
   - by runtime
   - by project
8. 增加公共 agent 默认配置：
   - workspace default agent
   - integration default agent
9. 扩展外部 issue sync 默认指派公共 agent。

验收：

- 外部系统创建 issue 可自动派给公共 agent。
- 多个用户同时使用公共 agent，不共享 workdir/session。
- 任务失败可追溯到 requester、agent、runtime。
- 公共 runtime 离线时任务不会永久卡住。

## 13. 安全设计

### 13.1 Token 安全

- 所有 token 只保存 hash。
- token 明文只在创建时展示一次。
- integration token 必须有 scope。
- daemon token 支持吊销。
- 飞书 app_secret 不落 DB 明文，使用 Secret Manager/KMS 引用。

### 13.2 数据安全

- 飞书消息不发送 secret、env、mcp_config、完整 agent 输出。
- 外部 issue sync 的 raw payload 可以按需脱敏后保存。
- 用户离职后禁止登录，并可清理/停用 runtime。
- 公共 runtime 任务必须 task 级隔离。

### 13.3 权限安全

- 飞书登录只证明身份，不直接授予 workspace 权限。
- workspace 成员权限仍由 Multica member 表控制。
- 外部 integration token 只能操作绑定 workspace。
- 公共 agent 管理权限只给 owner/admin。

## 14. 测试策略

### 14.1 后端单元测试

- 飞书 OAuth provider mock。
- external identity upsert。
- integration token hash/verify。
- issue sync 幂等。
- notification delivery retry。
- lark invitation delivery email -> open_id 解析。
- lark invitation delivery not-found/failed fallback。
- notification preference 兼容旧格式。

### 14.2 后端集成测试

- 飞书登录创建用户。
- 外部 issue sync 创建 issue。
- 外部 issue sync 重复请求 noop。
- 外部 issue sync 更新 issue。
- agent assignee 自动 enqueue task。
- 飞书通知 outbox 写入。
- 飞书邀请 delivery 写入和幂等重试。

### 14.3 前端测试

- login page 飞书按钮显示/隐藏。
- settings 通知渠道开关。
- integration token 创建页。
- runtime policy 设置页。

### 14.4 部署验证

- `make check`
- 内网 staging 飞书登录
- 本地 daemon 注册
- issue 创建与飞书通知
- 外部 sync API 压测与幂等验证

## 15. 观测与运维

新增日志字段：

```text
provider
tenant_key
workspace_id
external_type
external_id
integration_token_id
inbox_item_id
delivery_id
runtime_id
requester_id
invitation_id
```

新增指标：

- lark_oauth_success_total
- lark_oauth_failed_total
- notification_delivery_pending
- notification_delivery_failed_total
- lark_invitation_delivery_pending
- lark_invitation_delivery_failed_total
- integration_issue_sync_created_total
- integration_issue_sync_updated_total
- integration_issue_sync_noop_total
- runtime_registered_total
- runtime_offline_total

新增管理页面：

- 飞书集成状态
- integration token 管理
- notification delivery 失败列表
- 飞书邀请消息投递状态
- runtime owner/health 列表
- 外部 issue link 查询

## 16. 回滚策略

### 16.1 功能回滚

所有私有化能力通过 feature flag 控制。出现故障时按顺序关闭：

```text
LARK_NOTIFY_ENABLED=false
LARK_INVITE_NOTIFY_ENABLED=false
INTEGRATION_ISSUE_INGRESS_ENABLED=false
LARK_ORG_SYNC_ENABLED=false
LARK_AUTH_ENABLED=false
ENTERPRISE_RUNTIME_POLICY_ENABLED=false
```

### 16.2 数据回滚

- 新增表不影响核心表，可保留。
- 飞书通知 outbox 失败可暂停 worker。
- 飞书邀请消息失败可暂停 invite worker，保留邮件邀请路径。
- 外部 issue sync 创建的 issue 不自动删除，需业务确认。
- 已绑定飞书身份的 user 仍可通过应急登录进入。

### 16.3 上游同步回滚

- 每次同步 upstream 前创建 tag。
- CI 未通过不进入 release/internal。
- 私有 feature 分支逐个 merge，便于定位冲突来源。

## 17. 里程碑排期建议

| 阶段 | 周期 | 交付 |
| --- | --- | --- |
| Phase 0 | 2-3 天 | fork、CI、内网基础部署 |
| Phase 1 | 1-2 周 | 飞书登录与身份绑定 |
| Phase 2 | 1-2 周 | 飞书通知、邀请投递 outbox 与 worker |
| Phase 3 | 1-2 周 | 外部 issue sync API |
| Phase 4 | 1-2 周 | 飞书组织同步 |
| Phase 5 | 1-2 周 | 本地 runtime 企业治理 |
| Phase 6 | 2-4 周 | 内部公共 cloud runtime |

建议先完成 Phase 0-3，形成可用闭环：

```text
飞书登录 -> 邀请消息触达 -> 外部系统创建 issue -> 指派 agent -> runtime 执行 -> 飞书通知结果
```

## 18. 最小可用版本范围

MVP 只做以下能力：

1. 私有 fork + CI + 内网部署。
2. 飞书 OAuth 登录。
3. `user_external_identity` 绑定。
4. 飞书通知 channel，覆盖 issue assigned / mentioned / task failed。
5. 飞书邀请消息投递，支持用邮箱找到对应飞书用户。
6. integration token。
7. `/api/integrations/issues/sync` 幂等创建 issue。
8. 外部创建 issue 可默认指派 agent。
9. 本地 daemon runtime 正常注册与执行。

MVP 不做：

- 飞书组织全量同步。
- 公共 cloud runtime pool。
- 飞书 webhook 直接接入。
- 复杂双向状态同步。
- 成本中心报表。

## 19. 风险与应对

| 风险 | 应对 |
| --- | --- |
| 私有 fork 与上游冲突越来越多 | 通用扩展点上游化，私有实现集中目录 |
| 飞书 API 不稳定影响业务请求 | outbox + worker 异步投递 |
| 邮箱找不到对应飞书用户 | 标记 invitation delivery 失败并保留邮件邀请兜底 |
| 外部系统重复推送创建重复 issue | `external_issue_link` 唯一键幂等 |
| 公共 agent 上下文串扰 | task 级 worktree/container/session 隔离 |
| 离职员工 runtime 继续执行任务 | 组织同步禁用用户并吊销 daemon token |
| 飞书消息泄露敏感信息 | 消息摘要化、截断、字段 allowlist |
| 外部 token 泄露 | scope 最小化、过期、吊销、审计 |

## 20. 推荐落地顺序

第一步不要直接做复杂飞书 webhook 和 cloud runtime。最稳路径是：

1. 建立 fork 与同步机制。
2. 做飞书登录，解决“谁是员工”。
3. 做飞书通知，解决“任务状态触达”。
4. 做外部 issue sync，解决“任务从企业系统进 Multica”。
5. 治理本地 runtime，支持前期真实开发。
6. 再做公共 cloud runtime，支持后期规模化共享 agent。

这样每一步都能独立上线，也能随时关闭，不会一次性把私有化改造压到核心路径上。
