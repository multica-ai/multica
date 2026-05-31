# 单能力 Tasks

## 实现目标

交付统一数据导出入口、canonical manifest 构建器，以及从同一 manifest 派生的多格式导出。

## 前置依赖

- 先确认 manifest 字段和 `schema_version`。
- 先确认数据管理入口落点。
- 先确认 CSV/Excel/PDF 只做派生格式。

## 任务切片

### Task 1

- 目标：新增数据管理前端入口与导出页面。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/settings/components/` 或新的 data-management 目录
- 完成定义：
  - 用户能进入统一导出入口。
- 验证方式：
  - 路由与页面测试。

### Task 2

- 目标：定义导出 manifest 类型与前端请求模型。
- 文件：
  - `apps/workspace/src/shared/types/`
  - `apps/workspace/src/shared/api/client.ts`
- 完成定义：
  - manifest 与导出请求参数有稳定类型定义。
- 验证方式：
  - 类型与 hook 单测覆盖 format/range。

### Task 3

- 目标：补服务端导出 builder 与下载接口。
- 文件：
  - `server/cmd/server/router.go`
  - `server/internal/handler/`
  - `server/pkg/db/queries/`
- 完成定义：
  - 可以生成 JSON manifest，并派生其他格式。
- 验证方式：
  - handler / 集成测试覆盖 json/csv/xlsx/pdf 与错误路径。

### Task 4

- 目标：补回归与文档回写。
- 文件：
  - 相关测试目录
  - `docs/superpowers/specs/feature-audit/05-data-sync/`
- 完成定义：
  - manifest 版本、入口位置、支持格式都已回写。
- 验证方式：
  - 文档与测试同步更新。

## 执行顺序说明

- 先锁 manifest，再接接口和页面；不要反过来从 UI 倒推契约。

## 回写要求

- 回写 `overview.md` 的共享契约字段与当前状态。
- 回写 `spec.md` 的“缺口关闭情况”。
- 若 manifest 版本变化，联动回写 5.2 / 5.3 文档。
