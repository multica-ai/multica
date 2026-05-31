# 单能力 Tasks

## 实现目标

交付统一导入入口、服务端 dry-run / apply 管线，以及 issue CSV 适配器与 canonical JSON 导入。

## 前置依赖

- 先锁 manifest 字段与 schema version。
- 先确认导入入口落在数据管理页。
- 先确认 dry-run 结果结构。

## 任务切片

### Task 1

- 目标：新增统一导入页面与上传流程。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/settings/components/` 或 data-management 目录
- 完成定义：
  - 用户能从统一入口发起导入。
- 验证方式：
  - 路由与页面测试。

### Task 2

- 目标：定义导入 payload、dry-run、apply 结果类型。
- 文件：
  - `apps/workspace/src/shared/types/`
  - `apps/workspace/src/shared/api/client.ts`
- 完成定义：
  - 前端和后端共享导入契约。
- 验证方式：
  - 类型与 hook 单测。

### Task 3

- 目标：补服务端 import pipeline。
- 文件：
  - `server/cmd/server/router.go`
  - `server/internal/handler/`
  - `server/pkg/db/queries/`
- 完成定义：
  - 支持 dry-run / apply，至少接通 canonical JSON 与 issue CSV adapter。
- 验证方式：
  - handler / 集成测试覆盖成功、部分失败、schema 不兼容。

### Task 4

- 目标：迁移现有 issue 导入入口为 adapter。
- 文件：
  - `apps/workspace/src/features/issues/components/bulk-import-modal.tsx`
  - `apps/workspace/src/features/issues/components/issues-header.tsx`
- 完成定义：
  - issue 页保留快捷入口，但底层走统一导入管线。
- 验证方式：
  - issue 快捷入口集成测试。

## 执行顺序说明

- 先做统一契约和服务端 dry-run，再迁移现有 issue 导入入口。

## 回写要求

- 回写 `spec.md` 的完成度。
- 回写 `overview.md` 的共享契约和当前状态。
- 若 adapter 列表变化，补回 `research.md`。
