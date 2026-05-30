# 1.6 子任务与层级执行任务

## 1. 实现目标

- 基于现有 `parent_issue_id` 建立稳定的树形读模型与展示能力。

## 2. 前置依赖

- 无硬依赖，但 2.1/2.3 的时间统计展示会复用聚合口径。

## 3. 任务切片

### 切片 A：补服务端树形读模型

- 目标文件 / 目录：
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
- 完成定义：
  - 递归查询可返回 depth/path/聚合字段。
  - 移动父任务时有防环校验。
- 验证方式：
  - 查询测试覆盖多层树与防环。

### 切片 B：补前端树形展示

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/list-view.tsx`
  - `apps/workspace/src/features/issues/components/issue-detail.tsx`
- 完成定义：
  - 列表可展示缩进层级。
  - 详情页可展示多层子任务摘要。
- 验证方式：
  - 手动验证展开、收起、跳转。

### 切片 C：补聚合时间/进度口径

- 目标文件 / 目录：
  - `apps/workspace/src/shared/types/issue.ts`
  - 相关 time-tracking 查询目录
- 完成定义：
  - 响应中包含聚合时间或进度字段。
  - 前端文案说明口径。
- 验证方式：
  - 测试覆盖父子任务时间汇总。

## 4. 回写要求

1. 若层级模型从树扩成其他结构，先更新 `overview.md` 的共享约束。
2. 实现完成后回写 `spec.md` 的完成度。
