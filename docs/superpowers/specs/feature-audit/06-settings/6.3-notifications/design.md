# 单能力 Design

## 目标

- 保留现有 ntfy 通道配置，并为桌面通知、声音通知、提醒声音、通知时长补齐设计边界。
- 把“通知通道能力”和“设备投递偏好”拆成两层模型，避免错配。

## 非目标

- 不在本轮直接实现邮件通道。
- 不改写通知业务事件来源或 Inbox 路由。
- 不把浏览器权限状态硬同步到服务端。

## 当前架构基线

- 当前入口：`apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs`。
- 当前核心逻辑：`apps/workspace/src/features/settings/components/notifications-tab.tsx` `NotificationsTab`。
- 当前存储或状态：`server/pkg/db/generated/models.go` `NotificationPreference` 只保存 ntfy 配置。
- 当前 UI 或接口：`api.getNotificationPreferences()` / `updateNotificationPreferences()` / `testNotificationPreference()`。

### 代码证据

- `apps/workspace/src/features/settings/components/notifications-tab.tsx` `handleSave`：说明设置页已有保存入口。
- `server/internal/handler/notification_preference.go` `UpsertNotificationPreference`：说明服务端只会保存用户通道配置。
- `server/cmd/server/notification_listeners.go` `maybeSendNtfy`：说明当前发送通道唯一事实源是 ntfy。

## 缺口定义

- 清单要求的桌面通知、声音、提醒声音与通知时长都更接近“设备投递偏好”，但当前模型只有“用户级 ntfy 通道配置”。
- 若不先拆层，执行时很容易把浏览器权限、声音选择错误写入服务端通道表。

## 方案与权衡

### 方案 A：所有通知设置都写进服务端用户偏好

- 做法：在 `notification_preference` 里新增桌面、声音、时长字段。
- 优点：跨设备可同步。
- 风险：浏览器权限与声音资源明显是设备相关，强行同步会导致不同设备行为漂移。

### 方案 B：通道配置服务端化，设备投递偏好本地化，推荐

- 做法：保留 ntfy / 未来 email 等通道配置在服务端；新增本地 `notification delivery preferences` 保存桌面、声音、提醒声音、通知时长。
- 优点：符合当前 `NotificationPreference` 模型，也与 `use-pomodoro-settings` 的本地偏好模式一致。
- 风险：同一用户跨设备不会自动同步桌面与声音设置，需要产品接受这一边界。

### 方案 C：全部只做浏览器本地设置

- 做法：连 ntfy URL 也转成前端本地配置。
- 优点：前端实现简单。
- 风险：会破坏现有服务端通知监听器与多设备可漫游通道配置。

## 推荐方案

选择方案 B。

通知设置天然分成两层：服务端知道“该往哪里发”，本地设备知道“怎么在这台设备上响和显示”。当前仓库已经把 ntfy 通道做到了服务端，因此继续沿用这条主线最稳；桌面权限和声音选择则应留在本地设备偏好层。

## 数据模型或状态模型

```text
server: notificationChannels
├─ ntfy_url
├─ ntfy_token
└─ disabled_types

local: notificationDeliveryPreferences
├─ desktop_enabled
├─ sound_enabled
├─ sound_name
└─ display_duration_ms
```

- `desktop_enabled` 需要结合浏览器 Notification permission。
- `sound_name` 与 `display_duration_ms` 仅对本机生效。

## 接口契约

### 输入

- 通道配置输入：ntfy URL、token、通知类型开关。
- 本地投递偏好输入：桌面通知开关、声音开关、提醒声音、通知时长。

### 输出

- 服务端返回用户可漫游通道配置。
- 本地偏好存入浏览器本地存储，并在页面进入时恢复。
- 错误场景：桌面通知权限被拒绝时，不写入“已启用”；测试通道失败时保留草稿并给出错误反馈。

## UI 或交互流程

### 页面交互流

```text
/settings -> Notifications
  -> 读取 server channel config + local delivery prefs
  -> 用户配置 ntfy / 开关桌面通知 / 声音 / 时长
  -> 请求浏览器权限（如需要）
  -> 分别保存到 server 与 local
```

### 状态机

```text
[loading]
   -> [ready]
   -> [editing]
   -> [permission-prompt]
   -> [saving]
   -> [ready]
   -> [permission-denied]
```

### 数据变化流

```text
NotificationsTab
   -> api.updateNotificationPreferences() ----> server.notification_preference
   -> useLocalNotificationDeliveryPrefs() ---> localStorage
   -> runtime notify + browser notify
```

## 权限、边界条件、异常路径

- 谁可以使用：所有登录用户可配置自己的通知。
- 哪些输入非法：无效 ntfy URL、非法时长值、未知声音名称。
- 失败时如何处理：权限被拒绝则回退关闭桌面通知；通道保存失败时不覆盖本地草稿；测试发送失败时保持当前输入。

## 实现约束

- 不要把桌面权限结果写进 `notification_preference` 服务端表。
- 不要在本轮承诺 email channel；`product-overview.md` 只证明它是方向，不是当前已决设计。
- 必须保持现有 ntfy 通道测试能力。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 把设备偏好服务端化 | 多设备行为不一致 | 拆层：server channel + local delivery |
| 桌面权限被拒绝 | 用户误以为已启用 | 在 UI 中显式展示 permission 状态 |
| ntfy 与本地提醒重复轰炸 | 体验噪声增大 | 明确区分通道开关与设备行为开关，允许分别关闭 |

## 验收检查

1. 通知页能同时展示服务端通道配置和本地投递偏好。
2. ntfy 配置仍可测试发送。
3. 桌面通知、声音、提醒声音、通知时长都具备明确存储边界。
4. 文档明确 email 属于后续扩展，不在当前实现范围。
