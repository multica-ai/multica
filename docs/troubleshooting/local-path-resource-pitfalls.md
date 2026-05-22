# local_path Project Resource Implementation Pitfalls

## 问题现象

在实现 `local_path` 项目资源类型（让 project 支持绑定本地路径，并限制只有该路径所在机器的 agent 才能执行任务）时，遇到多个编译/类型错误，包括：

1. TypeScript 类型导出缺失导致 `Cannot find name 'LocalPathResourceRef'`
2. Import 路径错误，使用子模块路径而非 barrel index
3. React 组件中变量在声明前使用（hook 顺序问题）
4. Props 解构遗漏，已定义但未解构使用

## 背景

项目原本只支持 `github_repo` 资源类型。新增 `local_path` 类型涉及 10 个 phase 的全栈实现：DB migration → Go API → Daemon 注册 → 亲和性过滤 → 前端类型 → UI 组件 → Assignee Picker 过滤。前端部分需要在 `assignee-picker`、`create-issue`、`quick-create-issue` 三个组件中添加亲和性过滤逻辑。

## 根因

### 1. 类型导出遗漏

`packages/core/types/` 采用 barrel `index.ts` 集中导出模式。新增 `LocalPathResourceRef` 类型虽然定义在 `project.ts` 中，但未在 `index.ts` 的 re-export 列表中添加，导致消费方 `import` 失败。

### 2. Import 子路径 vs Barrel 路径

`projectResourcesOptions` 定义在 `packages/core/projects/resource-queries.ts`，通过 `packages/core/projects/index.ts` barrel re-export。直接 import 子路径 `@multica/core/projects/resource-queries` 在本项目的 pnpm workspace + internal packages 架构下不可解析（无 `package.json` `exports` 映射）。

### 3. React Hook 声明顺序

在 `quick-create-issue.tsx` 中，新增的 `affinityFilteredAgents` 计算依赖 `localPathDaemonIds` 和 `runtimeDaemonMap`，而 `visibleAgentIds` 又依赖 `affinityFilteredAgents`。如果亲和性过滤代码放在 `visibleAgentIds` 之后，则形成"使用在声明前"的错误。React hooks 和 useMemo 的声明顺序必须满足依赖拓扑。

### 4. Props 解构遗漏

给 `AssigneePicker` 组件新增了 `localPathDaemonIds` prop 类型定义，但忘记在函数参数解构中添加该字段，导致运行时 `localPathDaemonIds` 为 `undefined`。

## 涉及代码

- `packages/core/types/index.ts` — 类型 barrel 导出
- `packages/views/issues/components/pickers/assignee-picker.tsx` — AssigneePicker 组件
- `packages/views/modals/create-issue.tsx` — 创建 issue 弹窗
- `packages/views/modals/quick-create-issue.tsx` — 快速创建 issue 弹窗
- `packages/views/projects/components/project-resources-section.tsx` — 项目资源 UI

## 排查步骤

1. 类型导出问题：`pnpm typecheck` 报 `Cannot find name` → 检查 `index.ts` 导出列表 → 补充遗漏的 re-export
2. Import 路径问题：`pnpm typecheck` 报 `Cannot find module` → 确认 barrel `index.ts` 是否 re-export → 改用 barrel 路径 `@multica/core/projects`
3. Hook 顺序问题：`pnpm typecheck` 报 `Block-scoped variable used before declaration` → 画出变量依赖图 → 按拓扑排序调整声明位置
4. Props 解构遗漏：prop 类型已定义但运行时为 `undefined` → 检查函数参数解构列表 → 补充遗漏字段

## ⚠️ 注意事项

1. **新增类型必须同步更新 barrel `index.ts`**：在 `packages/core/types/` 下新增任何 export type，必须同时在 `index.ts` 添加 re-export，否则外部包无法 import。建议在 code review 时将此作为 checklist 项。
2. **Import 路径只用 barrel index**：本项目 internal packages 模式下，子模块路径无 `exports` 映射。始终用 `@multica/core/模块名`（对应 barrel `index.ts`），不要用 `@multica/core/模块名/子文件`。
3. **React 组件中变量声明顺序 = 拓扑序**：当新增 useMemo/useQuery 等 hook 并被后续变量依赖时，必须放在依赖它的变量之前。可以用注释标记依赖关系避免误排。移动代码块后务必检查是否有重复声明。
4. **Props 类型定义和解构必须同步**：在 TypeScript interface 中添加了新 prop 后，立即在函数参数解构中添加对应字段，避免"定义了但没解构"的 silent undefined。
5. **sqlc 的 Go 类型转换**：sqlc 生成的 JSONB 字段类型为 `pgtype.JSONB`，读取时需先检查 `.Valid`，再 `json.Unmarshal(.RawMessage, &target)` 而非直接类型断言。
