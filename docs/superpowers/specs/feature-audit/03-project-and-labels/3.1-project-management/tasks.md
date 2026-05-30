# 3.1 项目管理执行任务

## 1. 实现目标

- 在现有项目 CRUD 基础上补齐颜色、隐藏项目和视图/统计增强。

## 2. 前置依赖

- 统计口径依赖 `../01-task-management/1.1-basic-operations/` 的归档定义。
- 标签关联仍引用 `3.2-label-management/`。

## 3. 任务切片

### 切片 A：补项目 schema 与查询

- 目标文件 / 目录：
  - `server/migrations/`
  - `server/pkg/db/queries/project.sql`
  - `server/internal/handler/project.go`
  - `apps/workspace/src/shared/types/project.ts`
- 完成定义：
  - 项目支持 `color` 与 `hidden_at`。
  - 列表查询支持 hidden 过滤。
- 验证方式：
  - query / handler 测试覆盖 create/update/hide/unhide/list。

### 切片 B：补项目页视图与颜色编辑

- 目标文件 / 目录：
  - `apps/workspace/src/features/projects/components/projects-page.tsx`
  - `apps/workspace/src/features/projects/queries.ts`
- 完成定义：
  - 项目创建/编辑支持颜色。
  - 页面可切换全部、已完成、已隐藏。
- 验证方式：
  - 手动验证颜色保存、视图切换、隐藏/取消隐藏。

### 切片 C：统一统计口径

- 目标文件 / 目录：
  - `server/pkg/db/queries/project.sql`
  - `apps/workspace/src/features/projects/components/projects-page.tsx`
  - issue/time 统计相关目录
- 完成定义：
  - 统计默认排除隐藏项目和已归档 issue。
  - UI 显示口径说明。
- 验证方式：
  - 联动验证项目时间统计与 issue 生命周期一致。

## 4. 回写要求

1. 若项目视图枚举变化，先回写 `overview.md`。
2. 实现完成后回写 `spec.md` 的完成状态与交接说明。
