# tc-219 OPE-3457 频道消息收敛/释放拖拽

- **OPE Issue**: OPE-3457
- **Gitee PR**: (待提交)
- **特性**: 频道消息可在主消息区与回复区之间拖拽，实现"收敛"与"释放"两种结构重排。

## 行为

- **收敛（主消息 → 某消息的最新回复）**: 在主消息区长按某条消息后拖动到另一条主消息上，被拖消息成为目标消息线程的最新回复。
- **释放（回复 → 主消息区的最新消息）**: 在回复区长按某条回复后拖动到主消息区域，被拖回复成为主消息区的最新主消息。
- 长按激活（delay 250ms, tolerance 5px）：快速点击/拖动仍可选中文本、触发右键菜单，只有刻意按住再移动才启动拖拽。
- 主消息区列表容器是 release 落点（拖回复到任意空白或某条主消息上都释放）；主消息本身是 converge 落点。
- framed 的线程根消息和 system 消息不可拖拽。

## 语义决策

- **`created_at` 重置为 now()**：主消息/回复均按 `created_at ASC` 排序，重置时间戳保证被移动消息严格落在目标位置的"最新"（符合"最新回复/最新消息"）。代价：被移动消息的显示时间变为当前。
- **收敛需源消息无线程**：若被拖主消息已有线程（有回复或已关联 Issue），返回 409 拒绝——折叠已有人参与/已产出 Issue 的线程会导致回复/Issue 孤儿。源消息无线程时才允许收敛。
- **释放对回复无前置限制**：被释放回复的子回复（reply_to 指向它）留在原线程（扁平模型，无害）。
- **权限**：作者 或 canManage（镜像消息 edit/delete 的闸）。

## 后端

- 新增 `PATCH /api/channels/{id}/messages/{msgId}/move`，body `{ target_message_id: string|null }`。
  - `target` 非空 = 收敛（需源无线程、目标为主消息、非自身；按需创建目标线程）。
  - `target` 为空 = 释放（需源为回复）。
- 新增 sqlc 查询 `ReparentChannelMessage`（`UPDATE thread_id/reply_to_id/created_at/updated_at`，thread_id/reply_to_id 用 `sqlc.narg` 以表达 NULL）。
- WS 复用 `channel_message:updated` 事件 → 前端 `channelKeys.all(wsId)` 全量失效，其他客户端自动刷新。

## 前端

- `MessageDndProvider`：独立 `DndContext`（与频道列表的 DndContext 分离，避免 dnd-kit 不支持的嵌套），包住 `<main>` + 回复侧栏；long-press sensor；`DragOverlay` 消息预览；`onDragEnd` 调 `api.moveChannelMessage` + invalidate + toast。
- `MessageRow`：`useDraggable(msg:id)` + `useDroppable(msg:id)`，拖拽时 dim、成为 converge 目标时高亮。
- `PanelMessage`（非 framed 回复）：`useDraggable(reply:id)`。
- 主列表容器：`useDroppable(MSG_LIST_DROP_ID)`，回复拖拽时 ring 高亮提示释放。
- i18n：`channels.move.{converged,released,failed}`（en + zh-Hans，parity 测试通过）。

## 受影响文件

- `server/pkg/db/queries/channel.sql` — `ReparentChannelMessage` 查询。
- `server/pkg/db/generated/channel.sql.go` — sqlc 生成。
- `server/internal/handler/channel_v2.go` — `MoveChannelMessage` handler。
- `server/internal/handler/channel_move_test.go` — converge/release/权限/边界测试。
- `server/cmd/server/router.go` — `PATCH /messages/{msgId}/move` 路由。
- `packages/core/api/client.ts` — `moveChannelMessage` 方法。
- `packages/views/channels/components/channels-page.tsx` — `MessageDndProvider`、MessageRow/PanelMessage/主列表 droppable。
- `packages/views/locales/{en,zh-Hans}/channels.json` — `move` 文案。

## Commit SHA

- (待 maintainer 填充或 cherry-pick 后补)
