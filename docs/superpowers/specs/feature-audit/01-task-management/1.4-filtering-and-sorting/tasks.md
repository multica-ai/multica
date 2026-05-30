# 1.4 筛选与排序执行任务

## 1. 实现目标

- 固定筛选归属边界，并补齐 `updated_at` 排序。

## 2. 前置依赖

- 标签筛选能力依赖 `../03-project-and-labels/3.2-label-management/` 的实现。

## 3. 任务切片

### 切片 A：补排序枚举与实现

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/stores/view-store.ts`
  - `apps/workspace/src/features/issues/utils/sort.ts`
  - `apps/workspace/src/features/issues/components/list-view.tsx`
- 完成定义：
  - `SortField` 新增 `updated_at`。
  - `sortIssues` 支持 `updated_at`。
  - UI 菜单能选择该排序。
- 验证方式：
  - 单测或手动验证更新时间前后的排序变化。

### 切片 B：补筛选归属注释与文档回写

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/issue-list-page.tsx`
  - `apps/workspace/src/features/issues/utils/filter.ts`
  - `docs/superpowers/specs/feature-audit/01-task-management/overview.md`
- 完成定义：
  - 实现中不再隐含标签本地过滤。
  - 筛选归属说明回写完成。
- 验证方式：
  - 检查代码与文档均指向同一职责边界。

## 4. 回写要求

1. 若后续统一迁后端排序，先更新本设计包再动实现。
2. 实现完成后更新 `spec.md` 的“最后更新时间排序”状态。
