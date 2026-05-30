# 1.5 批量操作执行任务

## 1. 实现目标

- 在现有批量工具栏基础上新增批量编辑面板，承载项目迁移、标签增删与归档。

## 2. 前置依赖

- 批量归档依赖 1.1。
- 批量标签依赖 `../03-project-and-labels/3.2-label-management/`。

## 3. 任务切片

### 切片 A：扩展服务端批量接口

- 目标文件 / 目录：
  - `server/internal/handler/issue.go`
  - `server/pkg/db/queries/issue.sql`
  - 标签关系相关目录
- 完成定义：
  - 批量更新支持项目迁移与归档。
  - 批量标签增删有独立接口。
- 验证方式：
  - handler 测试覆盖 add/remove labels、move project、archive。

### 切片 B：新增批量编辑面板

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/batch-action-toolbar.tsx`
  - `apps/workspace/src/features/issues/components/`
- 完成定义：
  - 工具栏新增“批量编辑”入口。
  - 面板提供项目、标签、归档配置。
- 验证方式：
  - 手动验证多选 -> 编辑 -> 提交 -> 刷新。

### 切片 C：联动查询刷新与错误反馈

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/mutations.ts`
  - `apps/workspace/src/features/issues/queries.ts`
- 完成定义：
  - 成功后相关列表全部失效重取。
  - 失败时展示逐项反馈。
- 验证方式：
  - 手动验证成功与部分失败场景。

## 4. 回写要求

1. 若批量操作集合变化，更新 `overview.md` 的推进顺序与依赖。
2. 实现完成后回写 `spec.md` 的缺口状态。
