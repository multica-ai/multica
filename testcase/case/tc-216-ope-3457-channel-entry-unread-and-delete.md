# TC-216: 频道入口定位(已读→最新/未读→上次阅读)+ 频道列表删除按钮（OPE-3457）

Purpose: Verify (a) entering a channel lands at the latest message when fully read, and at the read/unread boundary ("jump to last read") when there are unread messages — with a "New messages" divider and a floating "Jump to last read" control — and that unread is only cleared once the user catches up (reaches the bottom); (b) the channel list shows a small delete button only for channels the user may manage (channel owner or workspace admin/owner), with a confirm dialog before the irreversible delete.

## Associated issue / PR
- OPE-3457（Channel 优化 v0.0.3 功能批次，本子特性随主线 PR 交付）
- Gitee PR: !430（同分支 `feat/ope-3457-channel-convert-issue`）

## One-line summary
频道入口按已读/未读分别定位到最新/上次阅读位置并在未读时提供跳转控件；频道列表对可管理的频道显示删除按钮（带确认弹窗）。

## Affected source files
- `server/pkg/db/queries/channel.sql` — ListChannels 新增 `first_unread_message_id` 子查询（首条顶层未读消息）。
- `server/pkg/db/generated/channel.sql.go` — sqlc 重新生成。
- `server/internal/handler/channel.go` — `ChannelResponse` 增 `FirstUnreadMessageID`，`channelListRowToResponse` 填充。
- `packages/core/types/channel.ts` — `ChannelSummary` 增 `first_unread_message_id`。
- `packages/views/channels/components/channels-page.tsx` — MessageList 入口定位/分隔线/跳转按钮/catch-up markRead；SortableChannelItem 删除按钮（canManage 显隐）+ AlertDialog 确认。
- `packages/views/locales/{en,zh-Hans,ja,ko}/channels.json` — `new_messages` / `jump_to_last_read` / `channel_deleted` / `delete_failed` / `delete_channel.*`。

## Permission requirement (delete button)
删除按钮仅对 `canManage` 为真的频道显示，镜像后端 `DeleteChannel` 的 `canManage()` 门槛（`channel.go`）：
- `wsAdmin`（请求者 workspace 角色为 owner 或 admin），**或**
- `channelOwn`（请求者是该频道的 owner 成员，即 `member_role === "owner"`）。

前端判定（`channels-page.tsx` SortableChannelItem）：`canDelete = channel.member_role === "owner" || isWorkspaceAdmin`，与后端完全一致。

## User flow

### A. 入口定位
1. （已读场景）在一个已读干净的频道有新消息前进入：验证落到最新一条消息（滚到底）。
2. （未读场景，少量未读 ≤ 一页）在另一用户的设备/会话向该频道发若干顶层消息制造未读；当前用户进入该频道：
   - 验证落在首条未读消息处（read/unread 边界），上方出现 "新消息"（New messages）分隔线。
   - 验证未读小圆点/未读状态在进入时**未**立即清除（catch-up 前）。
3. 向下滚动到底部：验证此时才清除未读（小圆点消失、分隔线消失）。
4. 再次进入（已读）：验证落到最新消息（滚到底）。
5. （未读场景，大量未读 > 一页）制造 >20 条顶层未读后进入：
   - 验证落到最新一页（底部），并出现浮动 "跳转到上次阅读"（Jump to last read）按钮。
   - 点击该按钮：验证消息重新以首条未读为中心加载并滚动到该边界，分隔线出现。
   - 滚到底部：验证未读被清除。

### B. 删除按钮
6. 以频道 owner（创建者）身份：验证其拥有的频道行悬停出现小删除按钮；非 owner 且非 workspace admin 的频道**不**出现该按钮。
7. 以 workspace admin/owner 身份：验证任意频道行均出现删除按钮。
8. 点击删除按钮：验证弹出确认弹窗（标题/描述含频道名/取消/删除）。点取消不删除；点删除后频道从列表消失并提示 "频道已删除"；若删除的是当前打开的频道，主区域回到空状态。
9. 以非 owner、非 admin 的普通成员身份：验证所有频道行均**不**显示删除按钮（无删除入口）。

## Expected results
- 已读→最新；未读→停在首条未读边界并显示分隔线 + 可跳转控件；未读仅在滚到底部（catch-up）后清除；多未读可通过浮动按钮跳转到上次阅读并加载该处。
- 删除按钮仅对 channel owner 或 workspace admin/owner 显示；删除需二次确认；删除后列表与主区域状态正确。
- 后端权限门槛（`canManage = wsAdmin || channelOwn`）与前端显隐一致；非授权用户前端无入口、后端 403 兜底。

## Notes for automation
- `first_unread_message_id` 来自 ListChannels 子查询（顶层消息，`thread_id IS NULL`，复用 `idx_channel_message_channel_toplevel`）；前端 `firstUnreadInPage = messages.some(m => m.id === firstUnreadMessageId)` 决定落点 vs 浮动按钮。
- markRead 不再在进入时立即触发（移除了 `ChannelsPage` 的 entry auto-markRead effect），改为 `MessageList` 在 isNearBottom 且边界已入页时触发；边界未入页（多未读）时即便在底部也不清除。
- `prevMsgCount` 在频道切换时重置为 0，避免新频道首屏被误判为新消息而滚到底。
