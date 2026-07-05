# ObitaPlus Right History Code Review

## 1. 概述

- 改动类型：前端页面功能增强、共享 chat 组件重构、测试补充、设计文档更新。
- 涉及文件：
  - `packages/views/chat/components/chat-page.tsx`
  - `packages/views/chat/components/chat-page.test.tsx`
  - `packages/views/chat/components/chat-window.tsx`
  - `packages/views/chat/components/chat-message-list.tsx`
  - `packages/views/chat/components/chat-message-list.test.tsx`
  - `packages/views/locales/*/chat.json`
  - `docs/system-design/frontend/obitaplus/design.md`

## 2. 需求分析

目标是在 `/{workspaceSlug}/obitaplus` 页面形成左侧工作区导航、中间聊天、右侧历史对话的页面结构，并修复 page 模式消息内容过于贴边的问题。历史对话应尽量复用现有 chat session 数据和交互能力。

## 3. 代码分析

- `ChatPage` 调整为横向布局，中间渲染 `ChatWindow variant="page"`，右侧渲染 `ChatSessionHistoryPanel`。
- `ChatWindow` 新增 `showSessionHistoryTrigger`，允许页面模式隐藏头部历史下拉，避免和右侧历史栏重复。
- `SessionDropdown` 增加 `panel` presentation，复用现有历史行、状态、重命名、删除、停止运行等逻辑。
- 右侧历史栏顶部增加可见的 `New chat` 主操作按钮，并隐藏中间聊天头部的重复 icon-only 新建入口。
- `ChatMessageList` page 模式使用居中 `max-w-4xl` 内容宽度，避免消息贴边。

## 4. 功能逻辑检查

- 需求一致性：满足三栏结构、右侧历史列表、聊天内容居中约束，以及更明确的新建会话入口。
- 完整度：历史列表展示标题、头像、运行/未读/时间状态，并复用选择、重命名、删除、停止运行交互。
- 边界充分性：legacy archived session 仍沿用现有过滤逻辑；窄屏右栏隐藏，避免挤压聊天区。

## 5. 代码设计检查

- 可读性：新增 `ChatSessionHistoryPanel` 和 `presentation` 分支，职责明确。
- 重复性：右侧列表复用原 `SessionDropdown` 内的行渲染和操作逻辑，避免新建第二套会话操作。
- 可测性：新增 page 布局和 page 消息宽度测试。

## 6. 代码质量检查

- 安全：未新增 API、鉴权或数据写入入口，沿用现有 workspace/session 权限边界。
- 性能：右栏复用 React Query 会话缓存，额外查询与中间聊天共享 query key，可被 TanStack Query 去重。
- 数据一致性：未改数据库和写链路；会话切换仍通过既有 Zustand active session。
- 线上风险：右栏在 `lg` 以下隐藏，降低响应式挤压风险。

## 7. 代码规范检查

- 命名语义：`ChatSessionHistoryPanel`、`showSessionHistoryTrigger`、`presentation="panel"` 能直接表达业务对象和展示职责。
- 注释一致性：本次新增/更新注释与 `docs/system-design/frontend/obitaplus/design.md` 保持一致。

## 8. 优化建议

- P2：后续如需在移动端访问历史列表，可增加一个页面级历史抽屉或复用头部下拉。

## 9. 总体评价

- 评分：4.5 / 5。
- 结论：实现与需求一致，复用现有 chat session 能力，未发现必须修复问题。
