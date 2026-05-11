# M1 项目驾驶舱 - Implementation Plan

## 改动范围

**纯前端改动，无需后端、无需 SQL migration，无需 make sqlc。**

---

## 文件变更清单

### 新增文件

#### `apps/workspace/src/features/projects/components/project-progress.tsx`

新增 `ProjectProgress` 组件，封装进度计算逻辑和进度条渲染：

```typescript
// Props
interface ProjectProgressProps {
  issues: Issue[];        // 已按 project_id 过滤的 issue 列表
  compact?: boolean;      // true = 列表卡片迷你模式, false = 详情页完整模式
}

// 内部逻辑
function computeProgress(issues: Issue[]) {
  // 排除 cancelled，分三组统计
  const active = issues.filter(i => i.status !== "cancelled");
  const done = active.filter(i => i.status === "done").length;
  const inProgress = active.filter(i => ["in_progress", "in_review"].includes(i.status)).length;
  const todo = active.filter(i => ["backlog", "todo", "blocked"].includes(i.status)).length;
  const total = done + inProgress + todo;
  return { done, inProgress, todo, total };
}

// 渲染：
// compact=false → 完整堆叠进度条(h-1.5) + 文字计数行
// compact=true  → 精简进度条(h-1) + "X/Y done" 文字
```

---

### 修改文件

#### `apps/workspace/src/features/projects/components/projects-page.tsx`

**修改 1：`ProjectDetailPanel` 组件**

位置：`relatedIssues` useMemo 之后，return JSX 的 header 区块之后。

变更点：
1. 在组件内引入 `useActorName` hook
2. 引入 `useWorkspaceStore` 取 members/agents（`useActorName` 已封装，直接用）
3. 在 header 下方（border-b 以下）、description form 上方，插入进度区块：
   ```
   <div className="border-b px-4 py-3 space-y-3">
     <ProjectProgress issues={relatedIssues} />
     <LeadDisplay leadType={project.lead_type} leadId={project.lead_id} />
   </div>
   ```
4. `LeadDisplay` 为内联函数组件（无需单独文件，仅在 projects-page.tsx 内部使用）：
   - 调用 `useActorName().getActorName(type, id)` + `getActorAvatarUrl(type, id)`
   - 渲染 Avatar（24px） + 名字，或"No lead"

**修改 2：`ProjectListItem` 组件**

位置：description 文字行下方，Badge 前。

变更点：
1. 接收 `issues: Issue[]` 作为新 prop
2. 在 description 行下方渲染 `<ProjectProgress issues={issues} compact />`
3. 若 `issues.length === 0`，隐藏进度行

**修改 3：`ProjectsPage` 主组件**

位置：传 `ProjectListItem` props 的地方。

变更点：
1. 在渲染 `ProjectListItem` 时，从 `useIssueStore` 传入已过滤的 issues：
   ```typescript
   const allIssues = useIssueStore((s) => s.issues);
   // 在 map 中：
   const projectIssues = allIssues.filter(i => i.project_id === project.id);
   ```

---

## 执行顺序

1. **创建** `project-progress.tsx`（独立组件，无依赖）
2. **修改** `projects-page.tsx`（引用新组件，添加 lead display）
3. **运行** `pnpm typecheck` 确认无类型错误
4. **运行** `pnpm test` 确认测试未退步

---

## 不需要的操作

- ❌ `make sqlc` — 无新 SQL
- ❌ 后端 handler 改动 — API 返回数据足够
- ❌ 新 zustand store — 不涉及新状态
- ❌ 新路由 — 路由不变

---

## 测试计划

- **无需新增单元测试**（进度计算逻辑简单，无分支歧义）
- 运行 `pnpm typecheck` + `pnpm test` 确认不退步
- 如有现有 projects 相关测试，修改后确认通过

---

## 风险点

1. `allIssues` 来自全局 store（WebSocket 同步），冷启动时可能为空 → 进度条显示空状态即可，无 crash 风险
2. `lead_id` 指向的 member/agent 已从 workspace 删除 → `useActorName` 返回"Unknown"，可接受
3. issues 全为 cancelled → total=0 → 进度条空状态，文字显示"No active issues"

---

STATUS: WAITING FOR USER APPROVAL
