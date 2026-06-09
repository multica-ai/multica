# Calendar Overlays Tasks

## 实现目标

在 My Time Calendar 上叠加只读 issue schedule / due overlay，同时保持现有 time entry DnD 行为。

## 前置依赖

- `MyTimeCalendarPage` 当前可正常展示和编辑 time entry。
- issue list API 支持按日期范围过滤。

## 任务切片

### Slice 1：overlay event builder

- 目标：新增 issue overlay event 转换工具。
- 目标文件：
  - `apps/workspace/src/features/time-tracking/utils/issue-overlay-events.ts`
  - `apps/workspace/src/features/time-tracking/utils/issue-overlay-events.test.ts`
- 完成定义：
  - start/end 转成 `issue-window`。
  - due_date 转成 `issue-due`。
  - terminal issue 可按参数排除。
- 验证方式：
  - `pnpm --filter @multica/workspace exec vitest run src/features/time-tracking/utils/issue-overlay-events.test.ts`

### Slice 2：事件 union 与渲染分层

- 目标：让 My Time Calendar 同时处理 time-entry 和 issue overlay。
- 目标文件：
  - `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
  - `apps/workspace/src/features/time-tracking/components/calendar/IssueOverlayEventCard.tsx`
- 完成定义：
  - `CalendarEvent` 或新 event type 包含 `kind`。
  - time entry 使用现有 `CalendarEventCard`。
  - issue overlay 使用新只读组件。
- 验证方式：
  - 组件测试或页面 smoke test 覆盖两类 event。

### Slice 3：overlay toggles

- 目标：增加 Tasks / Deadlines overlay 开关。
- 目标文件：
  - `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
- 完成定义：
  - Toggle 状态只影响 overlay，不影响 time entries。
  - 默认开启。
- 验证方式：
  - 组件测试覆盖 toggle 显隐。

### Slice 4：交互保护

- 目标：确保 issue overlay 不会触发 time entry 编辑、拖拽、resize。
- 目标文件：
  - `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
- 完成定义：
  - `draggableAccessor` 仅允许 time entry 且非 running。
  - `resizableAccessor` 仅允许 time entry 且非 running。
  - `onEventDrop` / `onEventResize` 对非 time entry 直接 return。
- 验证方式：
  - 相关测试覆盖 guard。

## 目标文件

- `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
- `apps/workspace/src/features/time-tracking/utils/issue-overlay-events.ts`
- `apps/workspace/src/features/time-tracking/components/calendar/IssueOverlayEventCard.tsx`
- `apps/workspace/src/features/issues/queries.ts`
- `apps/workspace/src/shared/types/api.ts`

## 验证方式

- 运行新增工具测试。
- 运行 My Time Calendar 相关测试。
- 如果用户明确要求完整验证，再运行 `make check`。

## 回写要求

- 如果实现需要新增后端日期过滤参数，更新 `design.md` 的接口契约。
- 如果 overlay 默认关闭，更新 `design.md` 的 UI 流程。
