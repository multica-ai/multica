# tc-218 OPE-3457 频道分组拖拽排序

- **OPE Issue**: OPE-3457
- **Gitee PR**: (待提交)
- **特性**: 频道列表中的分组支持拖拽以调整分组之间的顺序。仅一级分组——分组的挪动只影响分组间的位置，不影响分组内频道的顺序，也不允许嵌套。

## 行为

- 分组头作为拖拽手柄（hover 显示 grip 图标，`cursor-grab`）。
- 拖拽到另一分组上 = 插入到该分组之前（position 取与相邻分组的中点，DOUBLE PRECISION，仅被拖分组 position 变化）。
- 拖拽到列表根空白 / 非分组区域（频道、未分组区）= 追加到分组末尾。
- distance=5 激活约束：普通点击仍折叠/展开分组，右键仍出上下文菜单（重命名/删除分组），不与拖拽冲突。
- 与频道拖拽共用同一 `DndContext`：分组用 `useDraggable`+`useDroppable`，频道用 `useSortable`，二者互不干扰。

## 后端

- 复用既有 `PATCH /api/channels/groups/{groupId}/position`（`UpdateChannelGroupPosition` handler，body `{ position }`）。
- 频道列表查询已按 `group_position ASC, c.position ASC` 排序，分组重排后列表自动重排。

## 受影响文件

- `packages/views/channels/components/channels-page.tsx` — `ChannelGroupSection` 改 `useSortable` 风格的 `useDraggable`+`useDroppable`；`resolveGroupDropTarget` 计算 midpoint；`moveGroupMutation`；`RootDropZone`；`DragOverlay` 分组预览。
- `packages/core/api/client.ts` — 复用 `updateChannelGroupPosition`。

## Commit SHA

- (待 maintainer 填充或 cherry-pick 后补)
