# 单能力 Tasks

## 实现目标

在保留 ntfy 通道配置的前提下，补齐桌面通知、声音通知、提醒声音与通知时长的设计落点，并把它们拆成服务端通道层与本地投递层。

## 前置依赖

- 先确认设备级投递偏好接受本地存储，不要求跨设备同步。
- 邮件通道保持后续扩展，不纳入当前切片。

## 任务切片

### Task 1

- 目标：补本地投递偏好 hook 与 schema。
- 文件：`apps/workspace/src/features/settings/` 下新增 local notification preference hook / schema 文件。
- 改动：定义 `desktop_enabled`、`sound_enabled`、`sound_name`、`display_duration_ms` 默认值与校验。
- 完成定义：通知页可稳定读写本地投递偏好。
- 验证方式：Vitest 覆盖默认值、非法值回退与持久化恢复。

### Task 2

- 目标：重构通知页 UI，拆分通道区与设备区。
- 文件：`apps/workspace/src/features/settings/components/notifications-tab.tsx`
- 改动：保留 ntfy 区块；新增桌面、声音、提醒声音、时长区块；补权限状态反馈。
- 完成定义：用户能在一页内区分“发往哪里”和“本机怎么提醒”。
- 验证方式：组件测试覆盖权限拒绝、保存成功、测试发送失败等场景。

### Task 3

- 目标：保留并验证服务端通道路径。
- 文件：`apps/workspace/src/shared/types/notification-preference.ts`、`apps/workspace/src/shared/api/client.ts`、`server/internal/handler/notification_preference.go`、`server/cmd/server/notification_listeners.go`
- 改动：仅在通道配置层扩展时再触碰服务端；当前必须保持 ntfy 行为不回退。
- 完成定义：existing ntfy save/test/send path 可继续工作。
- 验证方式：`go test ./server/internal/handler -run TestNotificationPreference`；相关前端测试通过。

### Task 4

- 目标：回写审计台账。
- 文件：`docs/superpowers/specs/feature-audit/06-settings/6.3-notifications/spec.md`、`docs/superpowers/specs/feature-audit/06-settings/overview.md`
- 改动：更新实现证据、状态与交接说明。
- 完成定义：6.3 的“部分实现”状态被准确回写。
- 验证方式：人工核对证据路径与符号名。

## 执行顺序说明

先定义本地投递偏好，再重构设置页，最后验证服务端通道路径与文档回写。这样可以避免把本地字段误塞进服务端模型。

## 回写要求

- 实现后更新 `6.3-notifications/spec.md` 的证据与缺口。
- 更新 `06-settings/overview.md` 的优先级备注与状态。
- 若决定引入 email 通道，必须先更新 `design.md`，不能直接扩代码。
