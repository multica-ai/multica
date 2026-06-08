# M3: Global Search & Command Palette

## Goal

用户获得一个全局、快速的入口，可以搜索 issue、project、member，并直接跳转到对象或执行动作。搜索成为默认工作流的一部分，而不是某个页面内的局部过滤器。

---

## Existing Capabilities (as of exploration)

| 能力 | 现状 |
|------|------|
| Issue 局部搜索 | ✅ `GET /api/issues?search=<text>&limit=N`，后端用 `ILIKE` 匹配 title/description，支持 UUID/issue number 精准匹配 |
| Project 搜索 API | ❌ 不存在 search 参数；所有 project 通过 `useProjectsQuery()` 加载到 React Query 缓存 |
| Member 列表 | ✅ 已在 `useWorkspaceStore(s => s.members)` 内存中；含 name/email 字段 |
| CommandDialog | ✅ `components/ui/command.tsx` 已有完整实现（cmdk 依赖已安装） |
| 键盘快捷键系统 | ❌ 无全局快捷键管理；仅 editor 内有局部 metaKey 处理 |
| Modal 系统 | ✅ `features/modals/store.ts` Zustand store；search 需单独 store |

---

## Product Decisions

### 1. 触发方式

**双入口：**
- `Cmd+K` (Mac) / `Ctrl+K` (Win/Linux)：全局键盘快捷键，在 `DashboardLayout` 的 `useEffect` 中监听
- Sidebar header 新增搜索图标按钮（置于 workspace switcher 与 new-issue 按钮之间）

理由：键盘用户依赖 Cmd+K，鼠标用户需要可见入口；两者都不能缺。

### 2. 搜索范围 V1

| 分组 | 数据来源 | 策略 | 上限 |
|------|---------|------|------|
| Issues | `GET /api/issues?search=<q>&limit=8` | 后端搜索，debounce 200ms | 8 |
| Projects | React Query 缓存 (`queryKeys.projects.list`) | 前端 ILIKE filter，query 为空时显示全部 | 5 |
| Members | `useWorkspaceStore(s => s.members)` | 前端 filter by name/email | 5 |
| Actions | 静态列表 | 无 API | 6 |

**Actions 列表（静态）：**
- Create Issue → `useModalStore.getState().open("create-issue")`
- Go to Projects → `/projects`
- Go to Board → `/board`
- Go to Settings → `/settings`
- Go to My Work → `/my-work`
- Go to Inbox → `/`

**不包含（V1 范围外）：**
- Agent 搜索（数量少，可通过 Settings 页管理）
- Skill 搜索
- 全文描述搜索（成本高，V1 只做 title 匹配）

### 3. 结果渲染

```
[ 搜索框 ]
─────────────
Issues
  • MUL-42  Fix login button   (status badge)
  • MUL-7   Update dashboard   (status badge)
Projects
  • Backend API   (status badge)
Members
  • Alice Wang   alice@example.com
Actions
  • Create Issue        ⌘N
  • Go to Settings      
```

- 无结果时显示 "No results for '...'" （cmdk `CommandEmpty`）
- 搜索为空时：只显示 Actions + 最近访问的 Issues（V1 可先只显示 Actions）

### 4. 跳转行为

| 类型 | 行为 |
|------|------|
| Issue | `router.navigate({ to: "/issues/$id", params: { id } })` |
| Project | `router.navigate({ to: "/projects/$id", params: { id } })` |
| Member | `router.navigate({ to: "/settings" })` |
| Action-navigate | `router.navigate(...)` |
| Action-modal | `useModalStore.getState().open(...)` |

选中任一结果后关闭 dialog。

### 5. 后端策略

**V1 无需新增后端接口。**
- Issue：复用现有 `GET /api/issues?search=` 
- Project/Member/Actions：纯前端聚合

### 6. UI 组件

使用 `components/ui/command.tsx` 的 `CommandDialog` + 全套子组件（CommandInput, CommandList, CommandGroup, CommandItem, CommandEmpty, CommandShortcut）。不引入新 UI 依赖。

---

## State Management

新建 `features/search/store.ts`，单一职责：

```typescript
interface SearchStore {
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
}
```

不进 modal store（modal 有 stacking 逻辑，search 是独立 overlay）。

---

## Module Structure

```
apps/workspace/src/features/search/
  index.ts                     # 导出
  store.ts                     # useSearchStore
  global-search-dialog.tsx     # CommandDialog 主组件
  use-search-results.ts        # 聚合 issues/projects/members/actions
```

---

## Integration Points

| 文件 | 变更 |
|------|------|
| `features/layout/components/dashboard-layout.tsx` | 挂载 `<GlobalSearchDialog />`，添加 Cmd+K `keydown` 监听 |
| `features/layout/components/app-sidebar.tsx` | SidebarHeader 新增搜索触发按钮（Search icon） |
| `features/modals/store.ts` | 无变更（只在 action 触发时调用） |

---

## Exit Criteria

- [x] Cmd+K / Ctrl+K 打开全局搜索 dialog
- [x] Sidebar header 有可见搜索入口
- [x] 搜索 issue（title match）结果正确展示
- [x] 搜索 project（name match，client-side）
- [x] 搜索 member（name/email match，client-side）
- [x] Actions 静态列表可见并可执行
- [x] 选中结果后导航 + dialog 关闭
- [x] 无结果时有 empty state

---

## Out of Scope (V1)

- 搜索历史 / 最近访问
- 搜索结果持久化
- 键盘快捷键系统（全局 registry）
- Agent/Skill 搜索
- 全文描述搜索（后端需要额外索引）
