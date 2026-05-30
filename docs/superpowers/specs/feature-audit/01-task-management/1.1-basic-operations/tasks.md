# 1.1 基础任务操作执行任务

## 1. 实现目标

- 为 issue 补齐归档 / 恢复 / 永久删除的完整生命周期，并为已归档视图提供查询契约。

## 2. 前置依赖

- 无硬依赖，但 1.3、1.5 会直接复用本能力的接口与状态字段。

## 3. 任务切片

### 切片 A：补后端生命周期字段与接口

- 目标文件 / 目录：
  - `server/migrations/`
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
- 完成定义：
  - issue 表新增归档字段。
  - `ListIssues` / `CountListedIssues` 支持归档谓词。
  - 新增 archive / restore handler，删除前置校验到位。
- 验证方式：
  - handler / query 测试覆盖 archive、restore、delete 三条路径。

### 切片 B：补前端类型与数据请求

- 目标文件 / 目录：
  - `apps/workspace/src/shared/types/issue.ts`
  - `apps/workspace/src/features/issues/mutations.ts`
  - `apps/workspace/src/features/issues/queries.ts`
- 完成定义：
  - `Issue` 类型新增归档字段。
  - API client 暴露归档与恢复调用。
  - 列表查询可带归档过滤参数。
- 验证方式：
  - 类型检查通过；query key 在归档过滤切换时发生变化。

### 切片 C：补详情页与列表入口

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/issue-detail.tsx`
  - `apps/workspace/src/features/issues/components/issue-list-page.tsx`
  - `apps/workspace/src/router.tsx`
- 完成定义：
  - 详情页提供归档、恢复、永久删除入口。
  - 列表页存在“已归档”视图或等价入口。
- 验证方式：
  - 手动验证活跃 -> 归档 -> 恢复 -> 永久删除完整链路。

## 4. 回写要求

1. 实现完成后回写 `spec.md` 的完成状态。
2. 若归档规则影响统计或列表默认行为，同步更新 `../overview.md` 的共享约束。
