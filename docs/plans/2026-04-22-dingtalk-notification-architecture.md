# DingTalk Notification Channel Architecture Draft

## Goal

在保留现有 Inbox 体验的前提下，为 Multica 增加“用户在 Settings 绑定第三方账号（先支持钉钉），被 `@` 时按用户选择的通知渠道发送消息和链接”的扩展架构。

本草案基于本地 `multica` 仓库分析，并已核对 `v0.2.13..HEAD` 之间与通知 / Inbox / Settings 相关的关键文件无差异；因此以下结论可视为针对 `v0.2.13` 的架构草案。重点参考：

- `CONTRIBUTING.md`
- `apps/docs/content/docs/developers/architecture.mdx`
- `server/cmd/server/main.go`
- `server/cmd/server/subscriber_listeners.go`
- `server/cmd/server/notification_listeners.go`
- `server/internal/handler/inbox.go`
- `packages/core/inbox/*`
- `packages/views/inbox/*`
- `packages/views/settings/components/settings-page.tsx`
- `packages/views/settings/components/account-tab.tsx`
- `server/internal/handler/auth.go`

## Current Architecture

### 1. 事件源已经存在

当前通知相关的 domain event 已经由 handler/service 发出：

- `issue:created`
- `issue:updated`
- `comment:created`
- `reaction:added`
- `issue_reaction:added`
- `task:failed`

统一入口是 `Handler.publish(...)`，底层走 `server/internal/events/bus.go` 的**同步**进程内 event bus。

### 2. “谁该被通知”已经有一层现成逻辑

`server/cmd/server/subscriber_listeners.go` 会在事件发生时自动维护 `issue_subscriber`：

- issue creator 自动订阅
- assignee 自动订阅
- commenter 自动订阅
- issue description / comment 中被 `@mention` 的对象自动订阅

`server/cmd/server/notification_listeners.go` 再基于这些订阅关系和直接 mention 规则决定通知接收者：

- 对 assignee 的 direct notify
- 对 member subscribers 的 notify
- 对 `@mentioned member` 的 notify
- 对 parent issue subscribers 的补充 notify

注意：当前通知逻辑明确只给 **member** 发，不给 **agent** 发外部通知；这和本需求一致。

### 3. Inbox 是当前唯一通知落地点

当前 notification listeners 的输出不是抽象“通知”，而是直接写 `inbox_item`：

- `CreateInboxItem(...)`
- 发布 `inbox:new`
- `registerListeners(...)` 再把 inbox 事件按 recipient 定向推给对应用户的 WebSocket 连接

也就是说，当前链路本质是：

`domain event -> recipient resolution -> inbox_item -> personal websocket -> inbox UI`

### 4. Inbox 更像 in-app read model，不是未来所有渠道的总账

从实现上看，Inbox 很适合作为“应用内通知视图”，但不适合作为未来多渠道通知的 source of truth：

- `packages/core/inbox/queries.ts` 在前端按 `issue_id` 做去重，UI 只显示每个 issue 最新一条
- `server/internal/handler/inbox.go` 的 archive 是按 issue 级联归档 sibling inbox items
- `read/archived` 语义是给应用内收件箱用的，不等于外部渠道送达、已读、重试状态

结论：**Inbox 应保留为 channel = inbox 的投影，不应直接承担多渠道通知总模型。**

### 5. Settings 现有结构适合加 tab，但没有第三方绑定模型

`packages/views/settings/components/settings-page.tsx` 已支持：

- My Account / Workspace 分组
- account 侧 tab 扩展
- desktop 通过 `extraAccountTabs` 注入额外 tab

但当前数据模型里：

- `user` 只有 name/avatar/email + onboarding 字段
- 没有 `user.settings`
- 没有第三方账号绑定表
- 没有通知偏好表

因此“钉钉绑定”和“通知渠道选择”不应塞进 `workspace.settings`，也不应继续把字段散落在 `user` 表。

### 6. OAuth 只存在登录样板，不存在账号绑定样板

仓库里已有 Google OAuth 登录链路：

- 前端 callback 页
- 后端 code exchange + userinfo 获取

这说明：

- OAuth 型第三方接入在技术上可复用现有套路
- 但那套逻辑是“登录身份”，不是“已登录用户绑定外部账号”

所以钉钉绑定应该是**独立的 account linking flow**，不能直接复用登录接口的数据语义。

## Architectural Conclusion

当前 Multica 的通知层已经具备两个非常好的基础：

1. 通知触发与 UI 渲染之间已经隔着 event bus
2. recipient resolution 已经从 issue/comment handler 中抽离出来

但还缺三层能力：

1. **用户级渠道绑定模型**
2. **用户级通知偏好模型**
3. **渠道分发层 / delivery outbox**

因此推荐的演进方向不是“在现有 notification listener 里直接调钉钉 API”，而是：

`domain event -> recipient resolution -> canonical notification -> channel fan-out -> inbox / dingtalk / future channels`

## Proposed Architecture

### A. 抽出 Notification Domain，而不是继续把 Inbox 当通知本体

建议新增一个更通用的 notification domain，最小拆成两层：

1. `notification_event`
2. `notification_delivery`

建议语义：

- `notification_event`: 某个用户收到的一次通知事实
- `notification_delivery`: 某个通知通过某个渠道的一次投递记录

示例字段：

`notification_event`

- `id`
- `workspace_id`
- `recipient_user_id`
- `type`
- `severity`
- `issue_id`
- `comment_id` nullable
- `actor_type`
- `actor_id`
- `title`
- `body`
- `details jsonb`
- `created_at`

`notification_delivery`

- `id`
- `notification_event_id`
- `channel` (`inbox`, `dingtalk`, future: `email`, `feishu`, `wechat_work`)
- `status` (`pending`, `sent`, `failed`, `cancelled`)
- `attempt_count`
- `last_error`
- `payload_snapshot jsonb`
- `sent_at`
- `created_at`
- 唯一键：`(notification_event_id, channel)`

Inbox 继续存在，但角色变成：

- `notification_event` 的一个 in-app projection
- 或 phase 1 中先保留原 `inbox_item`，并由 inbox adapter 从 `notification_event` 写入

### B. 账号绑定与渠道偏好拆开建模

建议新增两类表，而不是一个 JSON 大字段：

`external_account_binding`

- `id`
- `user_id`
- `provider` (`dingtalk`)
- `external_user_id`
- `external_union_id/open_id` 或 provider 对应主标识
- `display_name`
- `access_token_encrypted`
- `refresh_token_encrypted`
- `token_expires_at`
- `status` (`active`, `expired`, `revoked`, `error`)
- `metadata jsonb`
- 唯一键：`(user_id, provider)`

`notification_channel_preference`

- `id`
- `user_id`
- `channel` (`inbox`, `dingtalk`)
- `event_type` (`mentioned`, future extensible)
- `enabled`
- `binding_id` nullable
- `created_at`
- `updated_at`
- 唯一键：`(user_id, channel, event_type)`

这样可以清晰区分：

- “我有没有绑定钉钉账号”
- “我是否希望 `mentioned` 事件发到钉钉”
- “Inbox 是否仍然保留”

### C. 保留现有 recipient resolution，新增 channel selection

当前 `notifySubscribers / notifyDirect / notifyMentionedMembers` 的规则已经够用，不建议重写业务判定。

建议把通知流水分成两步：

1. **Recipient resolution**
   - 根据 issue_subscriber、assignee、mention 规则找出应该通知的 member
2. **Channel selection**
   - 根据用户绑定状态和偏好，决定写哪些 delivery

也就是说，未来钉钉支持不应再重新实现一遍“解析 mention、过滤 actor、找 parent subscribers”。

### D. 外部渠道必须异步，不要阻塞当前同步 event bus

这是本次架构最重要的约束。

当前 `events.Bus.Publish(...)` 是同步调用，notification listener 运行在请求 goroutine 内。如果直接在 listener 中请求钉钉 API，会带来：

- comment / issue update 请求时延被外部接口拖慢
- 钉钉抖动会影响主业务写入
- 无法做可靠重试
- 无法做幂等和观测

因此建议：

- listener 只负责创建 `notification_event`
- 同步写 `inbox_item` 或同步写 `notification_delivery(channel=inbox)`
- 对 `dingtalk` 仅写 `notification_delivery(status=pending)`
- 由独立 dispatcher worker 异步发送

推荐链路：

`comment:created -> notification listener -> notification_event + inbox projection + pending dingtalk delivery -> dispatcher -> dingtalk adapter`

### E. 把 Inbox 当作第一个 channel adapter

为了后续渠道扩展，建议显式引入 channel adapter 概念：

- `InboxChannelAdapter`
- `DingTalkChannelAdapter`

其中 Inbox adapter 负责：

- 创建/更新 `inbox_item`
- 发 `inbox:new` WebSocket 事件

DingTalk adapter 负责：

- 从 binding 中拿 token / external id
- 组装文案
- 发送消息
- 回写 sent/failed 状态

这样未来新增 Feishu / 企业微信时，不需要再改 notification listener 的业务判定。

### F. 增加统一 link builder，并补 comment deep link

本需求要求“消息内容以及链接通知到用户选择好的通知渠道”。

当前仓库已经有：

- `MULTICA_APP_URL`
- `FRONTEND_ORIGIN`
- issue detail path builder

但当前 issue 页面并没有通用的 comment deep link 规范；只有 Inbox 内联态能通过 `details.comment_id` 高亮评论。

因此建议补两件事：

1. 服务端统一 link builder
   - issue link
   - comment link
2. Web 端支持 comment deep-link
   - 例如 `/:workspaceSlug/issues/:id?comment=<commentId>`
   - 页面载入后滚动并高亮目标 comment

否则 DingTalk 只能发 issue 链接，无法稳定直达被 `@` 的那条评论。

## Settings / API Proposal

### Settings IA

建议在 `My Account` 组下新增一个 tab：

- `Notifications`
  - 第三方账号绑定
  - 渠道开关
  - 事件级偏好

不建议放在 workspace settings，原因是：

- 钉钉绑定是 user-scoped，不是 workspace-scoped
- 同一个用户在多个 workspace 中应复用同一外部账号
- 事件偏好最终也更接近个人收件偏好

### API Sketch

建议新增用户级接口：

- `GET /api/me/notification-bindings`
- `POST /api/me/notification-bindings/dingtalk/start`
- `POST /api/me/notification-bindings/dingtalk/callback`
- `DELETE /api/me/notification-bindings/:bindingId`
- `GET /api/me/notification-preferences`
- `PATCH /api/me/notification-preferences`

如果后续需要 workspace 级开关，可再补：

- `workspace.settings.notifications_enabled_channels`

但 phase 1 不建议先引入 workspace 级策略。

## Recommended Rollout

### Phase 1: 打通最小闭环

范围只做：

- Settings 中支持绑定钉钉
- `mentioned` 事件支持 `inbox + dingtalk`
- delivery outbox + retry
- issue link 先可用，comment deep link 如果来得及一起做

这一阶段不要改动：

- assignee/status/priority 等其他通知类型
- email / 飞书 / 企业微信
- agent external notifications

### Phase 2: 收敛到统一 notification domain

把当前直接写 `inbox_item` 的逻辑逐步迁移为：

- 先写 `notification_event`
- 再通过 adapter 写 inbox / dingtalk

同时补：

- admin/debug 可观测页面或 CLI
- failed delivery retry policy
- dead-letter / manual retry

### Phase 3: 更多渠道和更多事件

当 `mentioned` 链路稳定后，再把这些类型接入多渠道：

- `issue_assigned`
- `new_comment`
- `task_failed`

并扩展更多 channel adapter。

## Main Risks

### 1. 同步 listener 直接调第三方接口

这是最不推荐的实现方式，会让通知问题变成主链路故障。

### 2. 把用户绑定塞进 `user` 表零散字段

短期看简单，长期会让 provider 扩展、token 轮换、状态管理都变脆。

### 3. 把个人通知偏好放进 `workspace.settings`

语义不对，而且会让同一用户跨 workspace 的行为不一致。

### 4. 把 Inbox 的 read/archive 状态当成外部渠道状态

Inbox 是应用内视图；DingTalk 送达、失败、重试需要独立 delivery state。

## Recommendation

建议把这项需求按下面的最小可落地单位推进：

1. 先新增 `Notifications` account tab
2. 建 `external_account_binding + notification_channel_preference + notification_delivery`
3. 让当前 notification listener 产出 canonical notification / outbox
4. 保留 Inbox 作为第一个 adapter
5. 新增 DingTalk adapter 和异步 dispatcher
6. 补 comment deep link

这样能在不推翻现有 Inbox 架构的前提下，演进到“多渠道通知层”，而不是把钉钉逻辑硬焊进现有 listener。
