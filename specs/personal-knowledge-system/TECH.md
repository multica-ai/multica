# Personal Knowledge System Tech Spec

## Context

本技术方案实现 [`PRODUCT.md`](./PRODUCT.md) 中定义的个人 Markdown note 知识库。核心约束是：V1 只服务当前登录用户自己的 personal note，不做团队 Wiki、AI 自动使用、导入导出、文件命名系统或删除行为。

调研基于 commit `45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c`：

- [`apps/workspace/src/router.tsx:1-43`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/router.tsx#L1-L43) 和 [`apps/workspace/src/router.tsx:279-368`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/router.tsx#L279-L368) 是 workspace SPA 的页面注册位置，现有 `skills`、`focus` 等页面都通过 protected route 挂载。
- [`apps/workspace/src/features/layout/navigation.ts:1-89`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/features/layout/navigation.ts#L1-L89) 定义侧边栏分组、active 判断和页面标题。Knowledge 入口应在这里加入，active 规则使用 `/knowledge` 前缀。
- [`apps/workspace/src/features/search/use-search-results.ts:17-89`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/features/search/use-search-results.ts#L17-L89) 当前只聚合 issues、projects、members。Knowledge note 搜索需要成为新的结果类型，并走 API 侧搜索正文。
- [`apps/workspace/src/features/search/global-search-dialog.tsx:23-169`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/features/search/global-search-dialog.tsx#L23-L169) 当前渲染全局搜索结果和 Actions。这里需要增加 Notes 分组和 `New note` action。
- [`apps/workspace/src/features/editor/markdown-codemirror-editor.tsx:35-51`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/features/editor/markdown-codemirror-editor.tsx#L35-L51) 已有 Markdown CodeMirror editor，支持 `defaultValue`、`onUpdate`、`debounceMs`、`focus()` 和读取 markdown 内容，适合作为 note 正文编辑器。
- [`server/cmd/server/router.go:203-312`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/server/cmd/server/router.go#L203-L312) 是 workspace-scoped protected API 注册位置。Knowledge API 应放在同一组内，继承 workspace membership 校验。
- [`server/internal/handler/handler.go:33-68`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/server/internal/handler/handler.go#L33-L68) 显示 Handler 统一持有 `Queries`、`DB`、`TxStarter` 等依赖；Knowledge handler 应延续这个模式。
- [`server/internal/handler/issue.go:311-435`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/server/internal/handler/issue.go#L311-L435) 展示列表 API 的查询参数解析、归档过滤、标签过滤、搜索和 pagination 模式，Knowledge 列表 API 应借用这套语义。
- [`server/migrations/001_init.up.sql:76-88`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/server/migrations/001_init.up.sql#L76-L88)、[`server/pkg/db/queries/label.sql:1-49`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/server/pkg/db/queries/label.sql#L1-L49) 和 [`server/internal/handler/label.go:30-84`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/server/internal/handler/label.go#L30-L84) 证明 issue labels 是 workspace 级标签，并已有 case-insensitive 复用/创建逻辑。Knowledge 必须复用 `issue_label`，不使用 `time_entry_label`。
- [`apps/workspace/src/features/issues/components/pickers/label-picker.tsx:104-180`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/features/issues/components/pickers/label-picker.tsx#L104-L180) 是可复用标签选择器，已经读取 workspace labels 并支持搜索/创建入口。
- [`apps/workspace/src/shared/query/keys.ts:5-101`](https://github.com/aircjm/multim/blob/45f097d54f2c0cc7ae6f7054f8bb0ec53e9ebd0c/apps/workspace/src/shared/query/keys.ts#L5-L101) 管理 React Query key 和 workspace 切换时的 query 清理。Knowledge 需要新增 root key 并加入 workspace scoped roots。

有没有更优雅的方式？这里最容易走偏的是把 Knowledge 放进 Skill 或复用 time-entry label。更优雅的方案是把 note 作为独立领域对象，只复用 workspace label 这一个跨领域能力；这样既满足当前个人知识库，又不提前承诺 agent/skill 使用方式。

## Proposed Changes

### Backend data model

新增迁移 `server/migrations/047_knowledge_note.up.sql` / `.down.sql`。

```sql
CREATE TABLE knowledge_note (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    version INT NOT NULL DEFAULT 1,
    archived_at TIMESTAMPTZ,
    archived_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_note_to_label (
    note_id UUID NOT NULL REFERENCES knowledge_note(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES issue_label(id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, label_id)
);
```

索引：

- `knowledge_note(workspace_id, user_id, archived_at, updated_at DESC)` 支撑默认列表。
- `knowledge_note(workspace_id, user_id, updated_at DESC)` 支撑全局搜索默认排除 archived。
- `knowledge_note_to_label(label_id, note_id)` 支撑标签过滤。
- V1 使用 `ILIKE` 搜索 title/content/label name，不引入 pg_trgm、embedding 或全文索引。后续 note 数量增长后再评估搜索索引。

`user_id` 是可见性边界。所有 get/list/update/archive/restore 查询都必须同时过滤 `workspace_id` 和 `user_id`，不能只依赖 workspace membership。

`version` 是自动保存和多窗口冲突的乐观锁。客户端每次保存发送当前 `version`；后端 `UPDATE ... WHERE id = $id AND workspace_id = $workspace_id AND user_id = $user_id AND version = $expected_version`，成功后 `version = version + 1`。没有更新到行时，后端读取当前远端 note 并返回 `409 conflict`。

### Backend SQL and handlers

新增 `server/pkg/db/queries/knowledge_note.sql`，生成 sqlc 代码后由 handler 使用。建议查询：

- `ListKnowledgeNotes`：参数包括 `workspace_id`、`user_id`、`search_text`、`label_ids`、`include_archived`、`archived_only`、`limit`、`offset`；默认 `archived_at IS NULL`；排序 `updated_at DESC`。
- `CountKnowledgeNotes`：和列表同条件。
- `GetKnowledgeNoteForUser`：`id + workspace_id + user_id`。
- `CreateKnowledgeNote`：创建 title/content，`user_id` 来自 `X-User-ID`。
- `UpdateKnowledgeNoteWithVersion`：title/content 可部分更新，乐观锁递增 version。
- `ArchiveKnowledgeNote` / `RestoreKnowledgeNote`：归档只写 `archived_at/archived_by`，恢复清空两者。
- `ListKnowledgeNoteLabels`、`AddKnowledgeNoteLabel`、`RemoveKnowledgeNoteLabel`、`ReplaceKnowledgeNoteLabels`。

新增 `server/internal/handler/knowledge_note.go`：

- `KnowledgeNoteResponse`：`id`、`workspace_id`、`user_id`、`title`、`content`、`snippet`、`version`、`labels`、`archived_at`、`archived_by`、`created_at`、`updated_at`。
- `CreateKnowledgeNoteRequest`：`title` 必填或后端 fallback；`content` 可空；`label_ids` 可空。默认 title 由前端用用户本地时间生成，后端 fallback 只用于异常调用。
- `UpdateKnowledgeNoteRequest`：`title?`、`content?`、`label_ids?`、`version`。`version` 必填，缺失返回 400。
- `KnowledgeNoteConflictResponse`：`error`、`code: "knowledge_note_conflict"`、`note`。
- 列表/搜索响应：`{ notes, total }`。

API 路由挂在 workspace-scoped group：

```text
GET    /api/knowledge-notes
POST   /api/knowledge-notes
GET    /api/knowledge-notes/{id}
PATCH  /api/knowledge-notes/{id}
POST   /api/knowledge-notes/{id}/archive
POST   /api/knowledge-notes/{id}/restore
POST   /api/knowledge-notes/{id}/labels
DELETE /api/knowledge-notes/{id}/labels/{labelId}
```

V1 不实现 DELETE route。即使数据库有 cascade，产品层也不暴露删除入口。

标签处理复用 `issue_label`：

- 选择已有标签时，校验 label 属于当前 workspace。
- 自由创建标签时复用 `findOrCreateLabel` 的语义，避免同 workspace 下同名标签重复。
- Note 标签关系写入 `knowledge_note_to_label`，不写 `issue_to_label`。

搜索片段：

- 后端基于 title/content/label 命中生成 `snippet`。若正文命中，取命中点前后固定字符窗口并压缩换行；若只有 title/label 命中，snippet 可为空。
- API 不返回正文 offset，前端不做精确滚动，满足 PRODUCT 44、45、51。

### Frontend feature module

新增 `apps/workspace/src/features/knowledge/`：

```text
features/knowledge/
  index.ts
  queries.ts
  mutations.ts
  components/knowledge-page.tsx
  components/knowledge-note-list.tsx
  components/knowledge-note-editor.tsx
  components/knowledge-empty-state.tsx
  hooks/use-knowledge-autosave.ts
  utils/default-title.ts
```

不新增 Zustand store。Note 列表、详情、搜索、创建和归档都使用 React Query，因为它们是 server state，且需要按 workspace/user/query 参数缓存和失效。

新增类型：

- `apps/workspace/src/shared/types/knowledge.ts`
- 从 `shared/types/index.ts` 导出。
- `KnowledgeNote` 复用 `IssueLabel` 类型作为 `labels` 元素。
- `KnowledgeNoteListParams` 支持 `search`、`label_ids`、`archived`、`include_archived`、`limit`、`offset`。

新增 API client 方法到 `apps/workspace/src/shared/api/client.ts`：

- `listKnowledgeNotes(params)`
- `getKnowledgeNote(id)`
- `createKnowledgeNote(data)`
- `updateKnowledgeNote(id, data)`
- `archiveKnowledgeNote(id)`
- `restoreKnowledgeNote(id)`
- `addKnowledgeNoteLabel(id, input)`
- `removeKnowledgeNoteLabel(id, labelId)`

新增 query keys：

- `queryKeys.knowledge.lists()`
- `queryKeys.knowledge.list(workspaceId, params)`
- `queryKeys.knowledge.detail(workspaceId, noteId)`
- 把 `"knowledge"` 加入 `WORKSPACE_SCOPED_ROOTS`。

### Routing and navigation

在 `router.tsx` 新增 `KnowledgePage` import 和 protected route：

```text
path: "knowledge"
component: KnowledgePage
```

在 `navigation.ts` 的 Tools group 增加 `Knowledge`，图标用 `BookOpenText` 或 `NotebookText`。`isWorkspaceNavActive` 对 `/knowledge` 和 `/knowledge/...` 返回 active。

Knowledge 页面 URL 使用 query params 承载页面状态：

```text
/knowledge
/knowledge?note=<noteId>
/knowledge?new=1
/knowledge?archived=1
/knowledge?q=<search>
/knowledge?label=<labelId>
```

`new=1` 是全局入口和导航加号的统一协议。页面检测到后立即调用 create mutation，使用 `Untitled YYYY-MM-DD HH:mm:ss` 创建 note，成功后 replace 到 `/knowledge?note=<id>` 并聚焦 title 或正文。

顶部 `New` 入口当前是 header 中的 `New issue` 按钮，不是菜单。实现时将它改为轻量 dropdown，保留 `New issue`，增加 `New note`；`New note` 导航到 `/knowledge?new=1`。

### Page interaction model

桌面：

```text
+-------------------------------------------------------------+
| Header: Knowledge                               Search New   |
+-----------------------------+-------------------------------+
| Note list                   | Selected note                  |
| - Search input              | - Title input                  |
| - Label filter              | - Labels                       |
| - Active / Archived toggle  | - Save status + last synced    |
| - New note button           | - Edit / Preview segmented UI  |
| - Notes sorted updated desc | - Markdown CodeMirror/preview  |
+-----------------------------+-------------------------------+
```

移动：

```text
List view                         Detail view
+------------------------+        +------------------------+
| Back hidden            |        | Back to Knowledge      |
| Search / filters / New |  tap   | Title / labels/status  |
| Note list              +------> | Editor / preview       |
+------------------------+        +------------------------+
```

编辑器：

- 正文编辑使用 `MarkdownCodeMirrorEditor`。
- 预览使用现有 markdown renderer（`apps/workspace/src/components/markdown/Markdown.tsx`）。
- 编辑/预览切换用 segmented control 或 tabs；编辑模式默认打开。
- title 用 shadcn `Input` 或 textarea-like input，避免标题过长撑破布局。

标签：

- `KnowledgeNoteEditor` 复用 `LabelPicker`，传入 note 的 `labels`。
- `onAdd` 调用 `addKnowledgeNoteLabel`；`onRemove` 调用 `removeKnowledgeNoteLabel`。
- 标签变更后同时 invalidate note detail、note lists、workspace labels。

### Autosave and conflict state

保存状态机（ASCII）：

```text
          local edit
   +---------------------+
   v                     |
[Saved] -> [Dirty] -> [Saving...] -> [Saved]
              |              |
              |              +--> [Failed to save]
              |                       |
              +-----------------------+
              |
              +--> [Conflict: updated elsewhere]
                         | keep local
                         v
                    [Saving... force]
                         |
                         v
                       [Saved]

[Conflict] -- reload remote --> [Saved]
```

实现要点：

- `useKnowledgeAutosave(note)` 持有本地 draft：`title`、`content`、`labels`、`version`、`lastSyncedAt`、`status`。
- title/content 输入触发 dirty，debounce 后 PATCH。
- 离开当前 note 或组件 unmount 时 flush 当前 dirty draft；flush promise 可在后台继续。失败时用 `sonner` toast 显示全局错误，并把 draft 写入本地恢复缓存。
- 本地恢复缓存使用 `localStorage`，key 包含 `workspaceId + userId + noteId`，只保存失败或未确认的 draft，不做长期离线同步。
- PATCH 成功后用服务端返回的 note 覆盖 query cache，更新 `version` 和 `lastSyncedAt`。
- PATCH 返回 409 时进入 conflict 状态，展示固定英文提示 `This note was updated elsewhere.`。
- 用户选择 “Keep local” 时，再发一次 PATCH，带当前远端 `version` 和本地 draft；用户选择 “Reload remote” 时丢弃本窗口 draft，用远端 note 覆盖编辑器。

数据流（ASCII）：

```text
User input
  -> KnowledgeNoteEditor local draft
  -> useKnowledgeAutosave debounce/flush
  -> ApiClient PATCH /api/knowledge-notes/{id}
  -> Handler validates workspace_id + user_id + version
  -> sqlc updates knowledge_note / knowledge_note_to_label
  -> Handler returns KnowledgeNoteResponse
  -> React Query cache updates list/detail/global search
```

### Global search

`useSearchResults` 增加 notes query：

- 输入非空时调用 `api.listKnowledgeNotes({ search, limit: 5 })`。
- notes 只返回当前用户可见且未归档的 note。
- `isLoading` 合并 issues 和 notes fetching 状态。

`GlobalSearchDialog` 增加：

- description 改为包含 notes。
- Notes 分组显示 title、snippet、labels、updated time。
- 选中 note 后关闭 dialog，导航到 `/knowledge?note=<id>`。
- Actions 增加 `New note`，导航 `/knowledge?new=1`。

### End-to-end flow

```text
New note from Command/Search
  -> /knowledge?new=1
  -> KnowledgePage creates "Untitled <local timestamp>"
  -> API stores note with user_id = current user
  -> URL replace /knowledge?note=<id>
  -> Editor focuses title/body
  -> Autosave PATCH uses version
  -> List/detail/search caches refresh
```

```text
Search note body
  -> Knowledge page or global search sends search text to API
  -> SQL matches title/content/label name with current workspace + user filters
  -> Handler builds snippet, no offset
  -> User opens result, note detail loads without precise scroll
```

## Testing and Validation

Backend Go tests:

- PRODUCT 28、41-45、62-64、84-85：`ListKnowledgeNotes` only returns current user’s notes, searches title/content/label, excludes archived by default.
- PRODUCT 34-40：creating/selecting labels uses `issue_label`; issue and knowledge see the same label name/color.
- PRODUCT 53-61、80、83：archive hides from default list, archived filter returns it, restore brings it back, title/content/labels persist.
- PRODUCT 76-79：stale `version` update returns 409 with `code = "knowledge_note_conflict"` and current remote note.
- PRODUCT 59-60：router has no DELETE route and handler tests do not expose delete behavior.

Frontend Vitest:

- PRODUCT 7-10：`default-title.ts` formats `Untitled YYYY-MM-DD HH:mm:ss` using local time.
- PRODUCT 1-6、95-96：`/knowledge?new=1` creates a note, replaces URL with selected note, and focuses editable area.
- PRODUCT 16-18、66-75：list sorting, empty states, loading states, detail not-found/unavailable state.
- PRODUCT 21-33：autosave status transitions through `Saving...`、`Saved`、`Failed to save`、`Last synced at <time>`；background save failure shows toast and preserves local draft.
- PRODUCT 34-40、82：label add/remove changes labels without changing title and invalidates workspace labels.
- PRODUCT 46-52、97：global search renders note results, opens selected note, and focus leaves the command dialog.
- PRODUCT 76-79：409 conflict shows `This note was updated elsewhere.` and both choices behave correctly.

E2E/manual validation:

- Desktop viewport：Knowledge 双栏布局，新建、编辑、标签、归档、搜索、预览可用。
- Mobile viewport：默认列表，打开 note 进入详情，返回列表可用。
- Keyboard：创建、搜索、标签选择、归档/取消归档、编辑器聚焦可通过键盘完成；保存状态不只依赖颜色。

Implementation verification:

- 修改 SQL 后运行 `make sqlc`。
- 后端改动至少运行 `cd server && go test ./internal/handler/...`。
- 前端改动至少运行 `pnpm --filter @multica/workspace exec vitest run` 相关测试和 `pnpm typecheck`。
- 最终按项目要求在实现完成后运行 `make check`。

## Risks and Mitigations

- 乐观锁冲突频繁：自动保存可能在多窗口中频繁 409。缓解方式是只在用户明确选择前阻止静默覆盖，并提供 keep local/reload remote 两个明确路径。
- 搜索性能：V1 使用 `ILIKE` 简化实现，note 量大后可能慢。通过 workspace/user/archived 索引先缩小集合，后续再加 pg_trgm 或全文索引。
- 本地 draft 恢复：localStorage 只作为失败保护，不作为离线系统。恢复入口应只在保存失败或重新进入 note 时出现，避免和服务端成功状态混淆。
- 标签复用：复用 `issue_label` 会让 issue 和 note 标签共享改名/颜色，这是产品要求；实现时 UI 文案应避免称它为 issue-only label。

## Follow-ups

- 团队可见性、agent 使用 note、RAG/embedding、来源字段、导入/导出、从现有内容提取 note 都按 PRODUCT 非目标后置。
- 如果后续加入删除，需要先更新 PRODUCT/TECH，再补 soft-delete 数据模型和归档后删除流程。
