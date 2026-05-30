# 单能力 Research

## 调研目标

1. 确认通知设置当前已经支持哪些通道和偏好。
2. 确认桌面通知、声音、提醒声音与通知时长为何仍是缺口。
3. 明确通知设置应如何区分“通道配置”与“设备投递偏好”。

## 现状链路

1. 入口：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs` 已挂载 `notifications` 页签。
2. 前端链路：`apps/workspace/src/features/settings/components/notifications-tab.tsx` `NotificationsTab` 读取并保存 ntfy URL、token 与 disabled types。
3. 服务端链路：`server/internal/handler/notification_preference.go` `GetNotificationPreference` / `UpsertNotificationPreference` 提供用户级通知偏好 API。
4. 发送链路：`server/cmd/server/notification_listeners.go` `maybeSendNtfy` 根据偏好决定是否推送 ntfy。
5. 输出结果：当前只完成 ntfy 通道与通知类型开关，不包含桌面、声音、提醒声音与通知时长。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `accountTabs` | 通知设置当前属于账号侧偏好入口。 |
| `apps/workspace/src/features/settings/components/notifications-tab.tsx` | `NotificationsTab` | 前端 UI 只围绕 ntfy topic URL、token 和通知类型开关。 |
| `apps/workspace/src/shared/types/notification-preference.ts` | `NotificationPreference` | 前端通知偏好模型只有 `ntfy_url`、`ntfy_token`、`disabled_types`。 |
| `server/pkg/db/generated/models.go` | `NotificationPreference` | 后端持久化模型同样只有 ntfy 相关字段和禁用类型。 |
| `server/internal/handler/notification_preference.go` | `UpsertNotificationPreference` | 当前 API 只支持 ntfy 通道配置的读写。 |
| `server/cmd/server/notification_listeners.go` | `maybeSendNtfy` | 运行时真实发送通道只有 ntfy。 |
| `product-overview.md` | `当前阶段的项目目标与展望` | 产品已明确“更强提醒能力”目标，且目标形态是邮件、ntfy 或多通道组合。 |

## 空搜索证据

| 路径 | 符号 / 搜索关键词 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings`、`server` | `rg(desktop notification|notification duration|sound notification|reminder sound)` | 未找到匹配，说明桌面/声音/时长设置尚未实现。 |
| `apps/workspace/src/features/settings`、`server` | `rg(桌面通知|声音通知|提醒声音|通知时长)` | 未找到匹配，说明中文命名路径下也没有残片。 |

## 数据模型或状态流

- `server/pkg/db/generated/models.go` `NotificationPreference`：服务端通道偏好是“用户级可漫游配置”。
- `apps/workspace/src/features/settings/components/notifications-tab.tsx` `isActive`：前端把“是否填写 ntfy URL”作为通道启用状态。
- 当前没有设备级通知投递模型，因此桌面权限、声音开关和通知时长没有挂载点。

## 边界条件

- 证据：`server/internal/handler/notification_preference.go` `requireUserID`；结论：当前通知通道配置按用户身份存储，不受 workspace 成员角色控制。
- 证据：`server/cmd/server/notification_listeners.go` `maybeSendNtfy`；结论：通道发送发生在服务端监听器，不是前端本地假动作。
- 证据：`product-overview.md` `当前阶段的项目目标与展望`；结论：通知能力需要向多通道扩展，但并未要求所有行为都服务端化。

## 未决问题

1. 桌面通知和声音设置是否按设备本地保存；现有证据更支持“本地设备偏好”而非服务端同步。
2. 邮件通道是否进入当前阶段；产品目标提到邮件，但仓库还没有通知设置层面的邮件配置接口。
3. 通知时长是否只作用于浏览器 toast / desktop notification，还是也影响移动端与 ntfy；当前未定义。
